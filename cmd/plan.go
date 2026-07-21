package cmd

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// planTestLimit caps how many covering tests plan lists. Generous on purpose —
// plan's worklist should name every covering test, not paginate them.
const planTestLimit = 500

// planClaimNote is appended to every non-degenerate text response so Claude
// never mistakes plan's structural worklist for a correctness gate.
const planClaimNote = "note: plan maps the edit worklist structurally — not a correctness gate (run make audit)."

// planGoldenPlaceholder is the neutral golden/testdata stub. Churn detection is
// a follow-up; for now plan only flags that fixtures may need a hand scan.
const planGoldenPlaceholder = "churn not analyzed here; scan fixtures by hand if this symbol shapes golden output"

// Change modes for `snipe plan --change`.
const (
	planChangeSignature = "signature"
	planChangeBehavior  = "behavior"
	planChangeDelete    = "delete"
)

var (
	planAt         string
	planID         string
	planMaxCallers int
)

// PlanResult is the ordered edit worklist for a proposed symbol change: the def
// site (with signature), the call sites grouped by package, and the covering
// tests. Message is set instead of the rest of the fields for degenerate paths
// (no index, symbol not found, ambiguous, zero callers on delete uses the
// SafeToDelete fields) — plan is never a gate, so every such path exits 0.
type PlanResult struct {
	Message      string         `json:"message,omitempty"`
	Symbol       string         `json:"symbol,omitempty"`
	ID           string         `json:"id,omitempty"`
	Change       string         `json:"change"`
	Def          *PlanDef       `json:"def,omitempty"`
	CallSites    []PlanPkgGroup `json:"call_sites,omitempty"`
	MustRemove   bool           `json:"must_remove,omitempty"`
	SafeToDelete bool           `json:"safe_to_delete,omitempty"`
	TestOnlyRefs int            `json:"test_only_refs,omitempty"`
	Tests        PlanTests      `json:"tests"`
	Golden       string         `json:"golden_placeholder,omitempty"`
	Truncated    int            `json:"truncated_call_sites,omitempty"`

	// TotalCallSites/TotalPkgs are the pre-truncation totals the header
	// reports ("def + N call sites in K pkgs"); CallSites may hold fewer once
	// --max-callers truncates. Kept out of the header's job in the text path.
	TotalCallSites int `json:"total_call_sites,omitempty"`
	TotalPkgs      int `json:"total_pkgs,omitempty"`
}

// PlanDef is the definition site of the symbol under change.
type PlanDef struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	ID        string `json:"id"`
	Signature string `json:"signature"`
}

// PlanPkgGroup is a set of call sites sharing a package (the ref file's
// directory). The single-footer truncation rule reports the global dropped
// count via PlanResult.Truncated, so no per-group count is carried here.
type PlanPkgGroup struct {
	Pkg   string     `json:"pkg"`
	Sites []PlanSite `json:"sites"`
}

// PlanSite is one call site. ID chains to the enclosing symbol's def (empty for
// a package-level initializer); RefID is always present so the site stays
// addressable even when ID is empty.
type PlanSite struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	ID        string `json:"id"`
	RefID     string `json:"ref_id"`
	Enclosing string `json:"enclosing"`
	Snippet   string `json:"snippet"`
	ASTCtx    string `json:"ast_ctx,omitempty"`
}

// PlanTests groups covering tests by hop distance.
type PlanTests struct {
	Direct     []PlanTest `json:"direct"`
	Transitive []PlanTest `json:"transitive"`
}

// PlanTest is one covering test function.
type PlanTest struct {
	File string `json:"file"`
	Line int    `json:"line"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// runPlan builds the ordered edit worklist for a symbol Claude is about to
// change. Like verify, every degenerate path (no index, symbol not found,
// ambiguous, malformed --at, unknown --id, no input) exits 0 with a plain
// message via writePlanMessage — never an error envelope.
func runPlan(change string, args []string) error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	// nil writer: suppress OpenStore's missing-index error envelope. plan
	// degrades to an "index unavailable" message and exits 0 (a USER
	// condition). root is still populated on error, so the message-writer has
	// what it needs.
	s, root, err := OpenStore(nil, cmdNamePlan)
	if err != nil {
		return writePlanMessage(root, start, change, "index unavailable: "+err.Error())
	}
	defer s.Close()

	symbolID, msg, err := resolvePlanSymbol(s.DB(), root, args)
	if err != nil {
		return w.WriteError(cmdNamePlan, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	if msg != "" {
		return writePlanMessage(root, start, change, msg)
	}

	defSym, err := query.LookupByID(s.DB(), symbolID)
	if err != nil {
		return w.WriteError(cmdNamePlan, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	if defSym == nil {
		return writePlanMessage(root, start, change, "no symbol with id "+symbolID)
	}
	recordSessionQuery(root, defSym.Name, defSym.FilePathRel, defSym.LineStart, defSym.Kind, cmdNamePlan)

	result := PlanResult{
		Symbol: defSym.Name,
		ID:     symbolID,
		Change: change,
		Def: &PlanDef{
			File:      planRelFile(defSym.FilePathRel, defSym.FilePath),
			Line:      defSym.LineStart,
			ID:        symbolID,
			Signature: defSym.Signature.String,
		},
		Golden: planGoldenPlaceholder,
	}

	// Tests: direct + transitive covering tests. Skipped for the delete
	// zero-caller fast path (which lists test-only refs instead).
	buildTests := func() error {
		testRows, err := query.FindTestsMulti(s.DB(), []string{symbolID}, false, planTestLimit, 0)
		if err != nil {
			return err
		}
		for i := range testRows {
			tr := &testRows[i]
			pt := PlanTest{
				File: planRelFile(tr.FilePathRel, tr.FilePath),
				Line: tr.LineStart,
				ID:   tr.ID,
				Name: tr.Name,
			}
			if tr.Hop == 2 {
				result.Tests.Transitive = append(result.Tests.Transitive, pt)
			} else {
				result.Tests.Direct = append(result.Tests.Direct, pt)
			}
		}
		return nil
	}

	// internalErr escalates a genuine query/SQL failure to a non-zero error
	// envelope (mirrors verify.go), distinct from the USER conditions that
	// degrade to an exit-0 message.
	internalErr := func(err error) error {
		return w.WriteError(cmdNamePlan, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	switch change {
	case planChangeBehavior:
		// No signature change: skip call sites entirely, keep tests.
		if err := buildTests(); err != nil {
			return internalErr(err)
		}

	case planChangeDelete:
		nonTest, testOnly, err := query.CountCallSites(s.DB(), symbolID)
		if err != nil {
			return internalErr(err)
		}
		refs, err := query.FindCallSites(s.DB(), symbolID)
		if err != nil {
			return internalErr(err)
		}
		if nonTest == 0 {
			// Safe-to-delete fast path: list the test-only refs and stop.
			result.SafeToDelete = true
			result.TestOnlyRefs = testOnly
			result.CallSites, _, _, _ = planBuildCallSites(planFilterRefs(refs, true), -1)
			break
		}
		result.MustRemove = true
		result.CallSites, result.Truncated, result.TotalCallSites, result.TotalPkgs =
			planBuildCallSites(planFilterRefs(refs, false), planMaxCallers)
		if err := buildTests(); err != nil {
			return internalErr(err)
		}

	default: // "signature"
		refs, err := query.FindCallSites(s.DB(), symbolID)
		if err != nil {
			return internalErr(err)
		}
		result.CallSites, result.Truncated, result.TotalCallSites, result.TotalPkgs =
			planBuildCallSites(planFilterRefs(refs, false), planMaxCallers)
		if err := buildTests(); err != nil {
			return internalErr(err)
		}
	}

	return writePlan(result, root, start)
}

// resolvePlanSymbol mirrors impact.go's resolution structure. USER conditions
// (malformed --at, symbol not found, ambiguous, no input) route through a
// returned message (exit 0); a genuine query failure returns a non-nil error
// so the caller can escalate it to a non-zero INTERNAL envelope. Returns
// (symbolID, "", nil) on success, ("", message, nil) to degrade, or
// ("", "", err) on an internal failure.
func resolvePlanSymbol(db *sql.DB, root string, args []string) (symbolID, msg string, err error) {
	switch {
	case planID != "":
		return planID, "", nil

	case planAt != "":
		pos, perr := query.ParsePosition(planAt)
		if perr != nil {
			return "", "invalid --at: " + perr.Error(), nil //nolint:nilerr // malformed --at is a USER condition — degrade to an exit-0 message, not an INTERNAL error
		}
		filePath := pos.File
		if filepath.IsAbs(filePath) {
			if rel, err := filepath.Rel(root, filePath); err == nil {
				filePath = rel
			}
		}
		sym := query.FindSymbolAtPosition(db, filePath, pos.Line)
		if sym == nil {
			return "", "no symbol found at " + planAt, nil
		}
		return sym.ID, "", nil

	default:
		if len(args) == 0 {
			return "", "provide a symbol name, --at position, or --id", nil
		}
		name := args[0]
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				return name, "", nil
			}
		}
		symbols, err := query.LookupByName(db, name)
		if err != nil {
			return "", "", err
		}
		if len(symbols) == 0 {
			return "", "no symbol named " + name, nil
		}
		if len(symbols) > 1 {
			return "", planAmbiguousMessage(name, symbols), nil
		}
		return symbols[0].ID, "", nil
	}
}

// planAmbiguousMessage renders a candidate list for an ambiguous name (D2:
// disambiguate rather than error out).
func planAmbiguousMessage(name string, symbols []query.SymbolRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous: %d symbols named %q — disambiguate with --id or --at:", len(symbols), name)
	for i := range symbols {
		sym := &symbols[i]
		fmt.Fprintf(&b, "\n  %s:%d  %s  %s.%s",
			planRelFile(sym.FilePathRel, sym.FilePath), sym.LineStart, sym.ID, sym.PkgPath, sym.Name)
	}
	return b.String()
}

// planFilterRefs splits FindCallSites rows on IsTest. wantTest=true keeps only
// test-file refs (delete fast path); false keeps only production call sites.
func planFilterRefs(refs []query.RefRow, wantTest bool) []query.RefRow {
	out := refs[:0:0]
	for i := range refs {
		if refs[i].IsTest == wantTest {
			out = append(out, refs[i])
		}
	}
	return out
}

// planBuildCallSites groups ordered refs by the ref file's package directory,
// preserving order (refs arrive sorted by file_path_rel, so packages are
// contiguous). When maxSites >= 0 it truncates the flattened ordered list to
// the first maxSites; maxSites < 0 means no cap. Returns the display groups,
// the number of sites dropped, and the PRE-truncation totals (total sites and
// distinct packages) the header reports.
func planBuildCallSites(refs []query.RefRow, maxSites int) (groups []PlanPkgGroup, truncated, totalSites, totalPkgs int) {
	totalSites = len(refs)
	seenPkg := map[string]struct{}{}
	for i := range refs {
		seenPkg[filepath.Dir(refs[i].FilePathRel)] = struct{}{}
	}
	totalPkgs = len(seenPkg)

	if maxSites >= 0 && len(refs) > maxSites {
		truncated = len(refs) - maxSites
		refs = refs[:maxSites]
	}
	idx := map[string]int{}
	for i := range refs {
		r := &refs[i]
		pkg := filepath.Dir(r.FilePathRel)
		gi, ok := idx[pkg]
		if !ok {
			gi = len(groups)
			idx[pkg] = gi
			groups = append(groups, PlanPkgGroup{Pkg: pkg})
		}
		groups[gi].Sites = append(groups[gi].Sites, PlanSite{
			File:      r.FilePathRel,
			Line:      r.Line,
			Col:       r.Col,
			ID:        r.EnclosingID.String,
			RefID:     r.ID,
			Enclosing: r.EnclosingName,
			Snippet:   strings.TrimSpace(r.Snippet),
			ASTCtx:    r.ASTCtx,
		})
	}
	return groups, truncated, totalSites, totalPkgs
}

// planRelFile prefers the relative path for output, falling back to absolute.
func planRelFile(rel, abs string) string {
	if rel != "" {
		return rel
	}
	return abs
}

// writePlan dispatches to the JSON envelope or the terse Claude-default text
// renderer per the global --format flag.
func writePlan(p PlanResult, root string, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		return writePlanJSON(p, root, start)
	}
	return writePlanText(p)
}

// writePlanMessage renders a degenerate-path result — always exit 0, never a gate.
func writePlanMessage(root string, start time.Time, change, msg string) error {
	return writePlan(PlanResult{Message: msg, Change: change}, root, start)
}

// writePlanJSON emits the result inside the standard envelope; the single
// result is the sole entry, consumers read `.results[0]` (mirrors writeVerifyJSON).
func writePlanJSON(p PlanResult, root string, start time.Time) error {
	resp := output.Response[PlanResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []PlanResult{p},
		Meta: output.Meta{
			Command:  cmdNamePlan,
			RepoRoot: root,
			Ms:       time.Since(start).Milliseconds(),
			Total:    1,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func writePlanText(p PlanResult) error {
	var b strings.Builder

	if p.Message != "" {
		fmt.Fprintln(&b, p.Message)
		_, err := os.Stdout.WriteString(b.String())
		return err
	}

	writePlanHeader(&b, p)
	writePlanDef(&b, p)

	switch {
	case p.Change == planChangeDelete && p.SafeToDelete:
		writePlanTestOnlyRefs(&b, p)
	case p.Change == planChangeDelete:
		writePlanCallSites(&b, p, "MUST REMOVE — every ref below, else the build breaks")
		writePlanTests(&b, p, "TESTS — drop or rewrite")
		writePlanGolden(&b, p)
	case p.Change == planChangeBehavior:
		writePlanTests(&b, p, "TESTS — no signature change; verify these still pass / add cases for new behavior")
		writePlanGolden(&b, p)
	default: // signature
		writePlanCallSites(&b, p, "CALL SITES — edit every one to match the new signature")
		writePlanTests(&b, p, "TESTS — update/verify after editing")
		writePlanGolden(&b, p)
	}

	fmt.Fprintln(&b, planClaimNote)

	_, err := os.Stdout.WriteString(b.String())
	return err
}

func writePlanHeader(b *strings.Builder, p PlanResult) {
	sites, pkgs := p.TotalCallSites, p.TotalPkgs
	ntests := len(p.Tests.Direct) + len(p.Tests.Transitive)

	switch {
	case p.Change == planChangeDelete && p.SafeToDelete:
		fmt.Fprintf(b, "plan %s · delete · safe to delete — 0 non-test callers, %d test-only ref%s\n",
			p.Symbol, p.TestOnlyRefs, verifyPlural(p.TestOnlyRefs))
	case p.Change == planChangeDelete:
		fmt.Fprintf(b, "plan %s · delete · MUST remove %d ref%s in %d pkg%s before deleting def\n",
			p.Symbol, sites, verifyPlural(sites), pkgs, verifyPlural(pkgs))
	case p.Change == planChangeBehavior:
		fmt.Fprintf(b, "plan %s · behavior change · no signature change — verify these %d test%s\n",
			p.Symbol, ntests, verifyPlural(ntests))
	default: // signature
		fmt.Fprintf(b, "plan %s · signature change · def + %d call site%s in %d pkg%s · %d test%s\n",
			p.Symbol, sites, verifyPlural(sites), pkgs, verifyPlural(pkgs), ntests, verifyPlural(ntests))
	}
	fmt.Fprintln(b)
}

func writePlanDef(b *strings.Builder, p PlanResult) {
	label := "DEF"
	if p.Change == planChangeDelete {
		label = "DEF (remove)"
	}
	fmt.Fprintf(b, "%s  %s:%d  %s\n", label, p.Def.File, p.Def.Line, p.Def.ID)
	if p.Def.Signature != "" {
		fmt.Fprintf(b, "  %s\n", p.Def.Signature)
	}
}

func writePlanCallSites(b *strings.Builder, p PlanResult, header string) {
	if len(p.CallSites) == 0 {
		return
	}
	fmt.Fprintln(b, header)
	for _, g := range p.CallSites {
		fmt.Fprintf(b, "%s (%d)\n", g.Pkg, len(g.Sites))
		for _, s := range g.Sites {
			id := s.ID
			if id == "" {
				id = s.RefID
			}
			enc := s.Enclosing
			if enc == "" {
				enc = "(pkg-init)"
			}
			line := fmt.Sprintf("  %s:%d  %s  %s  %s",
				filepath.Base(s.File), s.Line, id, enc, s.Snippet)
			if s.ASTCtx != "" {
				line += "  [" + s.ASTCtx + "]"
			}
			fmt.Fprintln(b, line)
		}
	}
	if p.Truncated > 0 {
		fmt.Fprintf(b, "  +%d more call sites (raise --max-callers)\n", p.Truncated)
	}
}

func writePlanTestOnlyRefs(b *strings.Builder, p PlanResult) {
	n, _ := planCountSites(p.CallSites)
	fmt.Fprintf(b, "test-only refs (%d)\n", n)
	for _, g := range p.CallSites {
		for _, s := range g.Sites {
			id := s.ID
			if id == "" {
				id = s.RefID
			}
			enc := s.Enclosing
			if enc == "" {
				enc = "(pkg-init)"
			}
			fmt.Fprintf(b, "  %s:%d  %s  %s\n", filepath.Base(s.File), s.Line, id, enc)
		}
	}
}

func writePlanTests(b *strings.Builder, p PlanResult, header string) {
	if len(p.Tests.Direct) == 0 && len(p.Tests.Transitive) == 0 {
		return
	}
	fmt.Fprintln(b, header)
	if len(p.Tests.Direct) > 0 {
		fmt.Fprintf(b, "direct (%d)\n", len(p.Tests.Direct))
		for _, t := range p.Tests.Direct {
			fmt.Fprintf(b, "  %s:%d  %s  %s\n", t.File, t.Line, t.ID, t.Name)
		}
	}
	if len(p.Tests.Transitive) > 0 {
		fmt.Fprintf(b, "transitive (%d)\n", len(p.Tests.Transitive))
		for _, t := range p.Tests.Transitive {
			fmt.Fprintf(b, "  %s:%d  %s  %s  (2-hop)\n", t.File, t.Line, t.ID, t.Name)
		}
	}
}

func writePlanGolden(b *strings.Builder, p PlanResult) {
	if p.Golden == "" {
		return
	}
	fmt.Fprintf(b, "GOLDEN / TESTDATA — %s\n", p.Golden)
}

// planCountSites returns the number of emitted sites and distinct packages.
func planCountSites(groups []PlanPkgGroup) (sites, pkgs int) {
	pkgs = len(groups)
	for _, g := range groups {
		sites += len(g.Sites)
	}
	return sites, pkgs
}
