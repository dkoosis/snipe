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

	"github.com/dkoosis/snipe/internal/query"
)

// Role is the CRUD classification for a function referencing the target type.
type Role string

const (
	RoleCreate  Role = "Create"
	RoleMutate  Role = "Mutate"
	RoleRead    Role = "Read"
	RoleDelete  Role = "Delete"
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
}

// FromRefRows projects []query.RefRow into []Ref.
func FromRefRows(rows []query.RefRow) []Ref {
	out := make([]Ref, 0, len(rows))
	for _, r := range rows {
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
				out = append(out, classifyFileScope(pats, r))
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

	// Snippet patterns.
	createLit   *regexp.Regexp // `&?Nug\s*\{`
	createNew   *regexp.Regexp // `\bnew\(Nug\)`
	createMake  *regexp.Regexp // `\bmake\(\[\]\*?Nug[\s,)]`
	deleteCall  *regexp.Regexp // high-confidence delete evidence
	deleteMeth  *regexp.Regexp // `\.(Delete|Remove|Drop|Purge)\w*\(`
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

func compilePatterns(typeName string) *patterns {
	q := regexp.QuoteMeta(typeName)
	p := &patterns{
		typeName:    typeName,
		createLit:   regexp.MustCompile(`&?\b` + q + `\s*\{`),
		createNew:   regexp.MustCompile(`\bnew\(\s*` + q + `\s*\)`),
		createMake:  regexp.MustCompile(`\bmake\(\s*\[\]\*?` + q + `[\s,)]`),
		deleteCall:  regexp.MustCompile(`(?i)\bdelete\s*\(|\bdb\.Exec\([^)]*DELETE\b|\.Delete\s*\(|\.Remove\s*\(|\.Drop\s*\(|\.Purge\s*\(`),
		deleteMeth:  regexp.MustCompile(`\.(Delete|Remove|Drop|Purge)\w*\s*\(`),
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
	if sig := r2SnippetCreate(p, refs); sig != "" {
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
func classifyFileScope(p *patterns, r Ref) Classification {
	c := Classification{
		FileRel:    r.FileRel,
		Line:       r.Line,
		IsTestFile: isTestOrGeneratedFile(r.FileRel),
	}
	if sig := r2SnippetCreateOne(p, r.Snippet); sig != "" {
		c.Role = RoleCreate
		c.Signal = "[R6 file-scope] " + sig
		return c
	}
	c.Role = RoleUnknown
	c.Signal = "[R8 no-enclosing] file-scope reference"
	return c
}

// --- individual rules --------------------------------------------------------

// R1: snippet contains high-confidence delete evidence.
func r1DeleteEvidence(p *patterns, refs []Ref) string {
	for _, r := range refs {
		if isFuncDeclLine(r.Snippet) {
			continue
		}
		if loc := p.deleteMeth.FindString(r.Snippet); loc != "" {
			return "[R1 delete-call] " + trimSignal(r.Snippet)
		}
		if strings.Contains(strings.ToLower(r.Snippet), "delete") &&
			p.deleteCall.MatchString(r.Snippet) {
			return "[R1 delete-call] " + trimSignal(r.Snippet)
		}
	}
	return ""
}

// R2: snippet shows type construction — &T{, T{, new(T), make([]T).
func r2SnippetCreate(p *patterns, refs []Ref) string {
	for _, r := range refs {
		if sig := r2SnippetCreateOne(p, r.Snippet); sig != "" {
			return sig
		}
	}
	return ""
}

func r2SnippetCreateOne(p *patterns, snippet string) string {
	// A function declaration line (`func F(...) *T {`) syntactically matches
	// `T{` but the brace is the function body, not a struct literal. Skip.
	if isFuncDeclLine(snippet) {
		return ""
	}
	if m := p.createLit.FindString(snippet); m != "" {
		return "[R2 snippet] " + trimSignal(snippet)
	}
	if m := p.createNew.FindString(snippet); m != "" {
		return "[R2 new()] " + trimSignal(snippet)
	}
	if m := p.createMake.FindString(snippet); m != "" {
		return "[R2 make()] " + trimSignal(snippet)
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

// isFuncDeclLine reports whether the snippet is a function/method declaration
// line rather than an expression. These lines end with `{` opening the body,
// not a struct literal, so R2/R1 snippet rules must ignore them.
func isFuncDeclLine(snippet string) bool {
	s := strings.TrimSpace(snippet)
	return strings.HasPrefix(s, "func ") || strings.HasPrefix(s, "func(")
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
