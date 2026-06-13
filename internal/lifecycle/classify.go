// Package lifecycle classifies function references to a Go type into CRUD
// roles (Create, Mutate, Read, Delete) to support `snipe lifecycle <Type>`.
//
// Classification is per-function (aggregating all refs within each enclosing
// function) using a rule table where evidence-based snippet signals beat
// name-based heuristics. See docs/progress.md for the design rationale.
package lifecycle

import (
	"regexp"
	"strings"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/query"
)

// Role is the CRUD classification for a function referencing the target type.
type Role string

const (
	RoleCreate Role = "Create"
	RoleMutate Role = "Mutate"
	RoleRead   Role = "Read"
	RoleDelete Role = "Delete"
	// RoleTypeUse holds file-scope references — var declarations, struct field
	// types, interface-assertion lines (`var _ Store = (*X)(nil)`). These
	// aren't unclassified CRUD ops; they're type-reference sites. Separated
	// from Unknown so a non-zero Unknown still flags a real classification gap.
	RoleTypeUse Role = "Type uses"
	RoleUnknown Role = "Unknown"
)

// Classification is the per-function result. Signal carries the rule id and
// the matching evidence so Claude can audit classifications without re-running
// queries. Mixed holds any secondary roles that also had evidence, sorted by
// rule rank, so callers like LoadOrCreate surface both Create and Read.
type Classification struct {
	EnclosingID   string
	EnclosingName string
	FileRel       string
	Line          int
	Role          Role
	Signal        string
	Mixed         []Role
	IsTestFile    bool
}

// Ref is the minimal slice of a [query.RefRow] the classifier needs. Kept as
// its own type so tests can construct fixtures without touching the DB.
type Ref struct {
	EnclosingID        string
	EnclosingName      string
	EnclosingSignature string
	FileRel            string
	Line               int
	Snippet            string
	ASTCtx             string // syntactic context from the indexer (index.Ctx* values)
}

// FromRefRows projects []query.RefRow into []Ref.
func FromRefRows(rows []query.RefRow) []Ref {
	out := make([]Ref, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		encID := ""
		if r.EnclosingID.Valid {
			encID = r.EnclosingID.String
		}
		out = append(out, Ref{
			EnclosingID:        encID,
			EnclosingName:      r.EnclosingName,
			EnclosingSignature: r.EnclosingSignature,
			FileRel:            r.FilePathRel,
			Line:               r.Line,
			Snippet:            r.Snippet,
			ASTCtx:             r.ASTCtx,
		})
	}
	return out
}

// Classify groups refs by enclosing function and classifies each group.
// Refs with no enclosing function are handled as file-scope (may become
// Create via rule R6) or Unknown.
func Classify(typeName string, refs []Ref) []Classification {
	pats := compilePatterns(typeName)

	// Group by enclosing_id. The empty string is a synthetic bucket for
	// file-scope refs, each treated independently (one Classification per ref).
	byEnc := make(map[string][]Ref)
	order := make([]string, 0)
	for _, r := range refs {
		if _, ok := byEnc[r.EnclosingID]; !ok {
			order = append(order, r.EnclosingID)
		}
		byEnc[r.EnclosingID] = append(byEnc[r.EnclosingID], r)
	}

	out := make([]Classification, 0, len(order))
	for _, id := range order {
		group := byEnc[id]
		if id == "" {
			// File-scope refs: classify each ref alone.
			for _, r := range group {
				out = append(out, classifyFileScope(r))
			}
			continue
		}
		out = append(out, classifyFunction(pats, id, group))
	}
	return out
}

// --- rule engine -------------------------------------------------------------

type patterns struct {
	typeName string

	// Signature patterns (name-heuristic rules R3/R5 read declared signatures,
	// which are not per-ref AST facts).
	returnsType *regexp.Regexp // signature `) (\*?Nug[\s,)])` etc.
	takesPtrT   *regexp.Regexp // signature takes `*Nug`

	// Name prefixes (camelCase-bounded).
	namePrefix map[string]*regexp.Regexp // "create" -> ^(New|Make|Build|Create)...
}

var nameVerbs = map[string][]string{
	"create": {"New", "Make", "Build", "Create"},
	"delete": {"Delete", "Remove", "Archive", "Drop", "Purge"},
	"mutate": {"Set", "Update", "Add", "Append", "Put", "Upsert", "Merge"},
}

// deleteCallVerbs match callee names that count as delete evidence when the
// ref appears as a call argument (ast_ctx "call:<Name>").
var deleteCallVerbs = regexp.MustCompile(`^(?:Delete|Remove|Drop|Purge)(?:[A-Z]|$)`)

func compilePatterns(typeName string) *patterns {
	q := regexp.QuoteMeta(typeName)
	p := &patterns{
		typeName:    typeName,
		returnsType: regexp.MustCompile(`\)\s*(?:\(\s*)?\*?\b` + q + `\b`),
		takesPtrT:   regexp.MustCompile(`\*\s*\b` + q + `\b`),
		namePrefix:  map[string]*regexp.Regexp{},
	}
	for kind, verbs := range nameVerbs {
		// ^(Verb1|Verb2|...)(?:[A-Z]|$) — camelCase boundary or end of name.
		alt := strings.Join(verbs, "|")
		p.namePrefix[kind] = regexp.MustCompile(`^(?:` + alt + `)(?:[A-Z]|$)`)
	}
	return p
}

// classifyFunction applies rules R1..R5, R7 to a non-empty group of refs that
// share an enclosing function. Returns the primary role plus any secondary
// rule matches as Mixed.
func classifyFunction(p *patterns, encID string, refs []Ref) Classification {
	head := refs[0]
	c := Classification{
		EnclosingID:   encID,
		EnclosingName: head.EnclosingName,
		FileRel:       head.FileRel,
		Line:          head.Line,
		IsTestFile:    isTestOrGeneratedFile(head.FileRel),
	}

	// Evaluate every rule; first match becomes primary, rest become Mixed.
	hits := []struct {
		role   Role
		signal string
	}{}

	if sig := r1DeleteEvidence(p, refs); sig != "" {
		hits = append(hits, struct {
			role   Role
			signal string
		}{RoleDelete, sig})
	}
	if sig := r2ConstructionCtx(refs); sig != "" {
		hits = append(hits, struct {
			role   Role
			signal string
		}{RoleCreate, sig})
	}
	if sig := r3NameSigCreate(p, head); sig != "" {
		hits = append(hits, struct {
			role   Role
			signal string
		}{RoleCreate, sig})
	}
	if sig := r4NameDelete(p, head); sig != "" {
		hits = append(hits, struct {
			role   Role
			signal string
		}{RoleDelete, sig})
	}
	if sig := r5NameSigMutate(p, head); sig != "" {
		hits = append(hits, struct {
			role   Role
			signal string
		}{RoleMutate, sig})
	}

	if len(hits) == 0 {
		c.Role = RoleRead
		c.Signal = "[R7 default] no create/mutate/delete signal"
		return c
	}

	c.Role = hits[0].role
	c.Signal = hits[0].signal

	// Dedupe roles for Mixed: only surface distinct secondary roles.
	seen := map[Role]bool{c.Role: true}
	for _, h := range hits[1:] {
		if seen[h.role] {
			continue
		}
		seen[h.role] = true
		c.Mixed = append(c.Mixed, h.role)
	}
	return c
}

// classifyFileScope handles refs with no enclosing function (package-level
// declarations). A snippet Create pattern promotes to Create (R6); otherwise
// Unknown (R8).
func classifyFileScope(r Ref) Classification {
	c := Classification{
		FileRel:    r.FileRel,
		Line:       r.Line,
		IsTestFile: isTestOrGeneratedFile(r.FileRel),
	}
	if sig := r2ConstructionCtxOne(r); sig != "" {
		c.Role = RoleCreate
		c.Signal = "[R6 file-scope] " + sig
		return c
	}
	c.Role = RoleTypeUse
	c.Signal = "[R8 type-use] file-scope reference"
	return c
}

// --- individual rules --------------------------------------------------------

// R1: ref is an argument of a delete-verbed call (ast_ctx "call:DeleteX"),
// the builtin delete(), or an Exec/Query call carrying DELETE SQL.
func r1DeleteEvidence(_ *patterns, refs []Ref) string {
	for _, r := range refs {
		name, ok := strings.CutPrefix(r.ASTCtx, index.CtxCallPrefix)
		if !ok {
			continue
		}
		if name == "delete" || deleteCallVerbs.MatchString(name) {
			return "[R1 delete-call] " + trimSignal(r.Snippet)
		}
		// db.Exec("DELETE FROM ...", nug.ID) — call name carries no verb;
		// the SQL string does.
		if (name == "Exec" || name == "ExecContext") &&
			strings.Contains(strings.ToUpper(r.Snippet), "DELETE") {
			return "[R1 delete-call] " + trimSignal(r.Snippet)
		}
	}
	return ""
}

// R2: the indexer recorded the ref as type construction — the type position
// of a composite literal, or an argument of new()/make().
func r2ConstructionCtx(refs []Ref) string {
	for _, r := range refs {
		if sig := r2ConstructionCtxOne(r); sig != "" {
			return sig
		}
	}
	return ""
}

func r2ConstructionCtxOne(r Ref) string {
	switch r.ASTCtx {
	case index.CtxCompositeLit:
		return "[R2 lit] " + trimSignal(r.Snippet)
	case index.CtxNew:
		return "[R2 new()] " + trimSignal(r.Snippet)
	case index.CtxMake:
		return "[R2 make()] " + trimSignal(r.Snippet)
	}
	return ""
}

// R3: constructor-style name AND signature returns T or *T.
func r3NameSigCreate(p *patterns, r Ref) string {
	if !p.namePrefix["create"].MatchString(r.EnclosingName) {
		return ""
	}
	if !p.returnsType.MatchString(r.EnclosingSignature) {
		return ""
	}
	return "[R3 name+sig] " + r.EnclosingName + " returns " + p.typeName
}

// R4: name starts with Delete/Remove/Archive/Drop/Purge (camelCase-bounded).
func r4NameDelete(p *patterns, r Ref) string {
	if !p.namePrefix["delete"].MatchString(r.EnclosingName) {
		return ""
	}
	return "[R4 name] " + r.EnclosingName
}

// R5: mutating verb prefix AND signature takes *T.
func r5NameSigMutate(p *patterns, r Ref) string {
	if !p.namePrefix["mutate"].MatchString(r.EnclosingName) {
		return ""
	}
	if !p.takesPtrT.MatchString(r.EnclosingSignature) {
		return ""
	}
	return "[R5 name+sig] " + r.EnclosingName + " takes *" + p.typeName
}

// --- helpers -----------------------------------------------------------------

func trimSignal(snippet string) string {
	s := strings.TrimSpace(snippet)
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}

// isTestOrGeneratedFile detects test files and code-generated files by path
// suffix. These are bucketed separately in the output so they don't drown the
// real lifecycle.
func isTestOrGeneratedFile(path string) bool {
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	if strings.HasSuffix(path, "_gen.go") {
		return true
	}
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	if strings.HasPrefix(base, "zz_generated_") {
		return true
	}
	return false
}
