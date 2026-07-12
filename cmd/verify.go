package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/risk"
)

// verifyTestLimit caps how many covering tests FindTests returns per changed
// symbol. Generous on purpose — verify isn't paginated like `tests`, and a
// low cap would silently drop covering tests from the run set.
const verifyTestLimit = 500

// verifyClaimNote is appended to every non-degenerate text response so
// Claude never mistakes verify's structural mapping for a correctness gate.
const verifyClaimNote = "note: verify maps tests to changed symbols structurally — not a correctness gate (run make audit)."

// VerifyResult is the structural test-coverage report for a diff: which
// changed funcs/methods have covering tests, which don't, and the minimal
// `go test` invocations exercising the covered ones. Message is set instead
// of the rest of the fields for degenerate cases (empty diff, diff
// unavailable, no callable symbols changed) — it is never a "you're safe"
// signal, only "there was nothing structural to check."
type VerifyResult struct {
	Message        string          `json:"message,omitempty"`
	ChangedSymbols int             `json:"changed_symbols"`
	Covered        int             `json:"covered"`
	Uncovered      []string        `json:"uncovered,omitempty"`
	Packages       []VerifyPackage `json:"packages,omitempty"`
}

// VerifyPackage groups covering tests by the package that runs them, with a
// paste-ready `go test` invocation joining their names with `|`.
type VerifyPackage struct {
	Pkg   string   `json:"pkg"`
	Run   string   `json:"run"`
	Tests []string `json:"tests"`
}

// runVerify turns a diff into the minimal go test invocations covering its
// changed funcs/methods, plus the sharp "these changed symbols have no test"
// signal. base=="" diffs the working tree against HEAD; otherwise it diffs
// base against HEAD (branch-vs-base). Never a gate: every degenerate path
// (no index, no git, empty diff, no callable symbols) exits 0 with a message
// rather than an error, mirroring runRisk's degrade-not-fail contract.
func runVerify(base string) error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	s, root, err := OpenStore(w, cmdNameVerify)
	if err != nil {
		return err
	}
	defer s.Close()

	var changes []risk.FileChange
	var ok bool
	if base == "" {
		changes, ok = risk.WorkingTreeChanges(root)
	} else {
		changes, ok = risk.RefChanges(root, base, "HEAD")
	}
	if !ok {
		return writeVerifyMessage(root, start, "diff unavailable — git missing, not a work tree, or ref unresolved")
	}

	changeMap := verifyChangeMap(root, changes)
	if len(changeMap) == 0 {
		return writeVerifyMessage(root, start, "no Go changes — nothing to verify")
	}

	symbols, err := query.FindOverlappingSymbols(s.DB(), root, changeMap)
	if err != nil {
		return w.WriteError(cmdNameVerify, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	changedFuncs, changedTestFuncs := verifySplitSymbols(symbols)
	if len(changedFuncs) == 0 && len(changedTestFuncs) == 0 {
		return writeVerifyMessage(root, start, "no changed funcs/methods to verify")
	}

	v, err := verifyBuildResult(s.DB(), changedFuncs, changedTestFuncs)
	if err != nil {
		return w.WriteError(cmdNameVerify, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	return writeVerify(v, root, start)
}

// verifyChangeMap converts risk.FileChange (repo-relative paths, head-side
// line ranges) into the map[absPath][]query.LineRange shape
// FindOverlappingSymbols expects, dropping non-.go files and pure deletions.
func verifyChangeMap(root string, changes []risk.FileChange) map[string][]query.LineRange {
	changeMap := make(map[string][]query.LineRange)
	for _, fc := range changes {
		if !strings.HasSuffix(fc.Path, ".go") || len(fc.LineRanges) == 0 {
			continue
		}
		ranges := make([]query.LineRange, len(fc.LineRanges))
		for i, r := range fc.LineRanges {
			ranges[i] = query.LineRange{Start: r[0], End: r[1]}
		}
		changeMap[filepath.Join(root, fc.Path)] = ranges
	}
	return changeMap
}

// verifySplitSymbols filters overlapping symbols to callable kinds (func/
// method — only they have call-graph edges) and buckets them into
// changedFuncs (need coverage) and changedTestFuncs (Test*/Benchmark*/Fuzz*/
// Example* in a _test.go file — Claude edited a test, so it goes straight
// into the run set rather than being scored for coverage). A non-test helper
// changed inside a _test.go file is dropped: it isn't itself a test, and it
// has no call-graph edges into FindTests' target set.
func verifySplitSymbols(symbols []query.SymbolRow) (changedFuncs, changedTestFuncs []query.SymbolRow) {
	for i := range symbols {
		sym := &symbols[i]
		if sym.Kind != output.KindFunc && sym.Kind != output.KindMethod {
			continue
		}
		isTestFile := strings.HasSuffix(sym.FilePath, "_test.go")
		switch {
		case isTestFile && verifyIsTestFuncName(sym.Name):
			changedTestFuncs = append(changedTestFuncs, *sym)
		case isTestFile:
			// helper, not a test entry point — no coverage signal to attach it to
		default:
			changedFuncs = append(changedFuncs, *sym)
		}
	}
	return changedFuncs, changedTestFuncs
}

// verifyIsTestFuncName mirrors testFuncFilterFor's SQL GLOB predicate
// (internal/query/tests.go) for use against a symbol name already in hand.
func verifyIsTestFuncName(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Example")
}

// verifyBuildResult attributes coverage per changed symbol — a per-symbol
// FindTests loop, not FindTestsMulti, because FindTestsMulti's flat
// []TestRow has no field tying a row back to the symbol that produced it, so
// a zero-coverage symbol couldn't be named. Changed test funcs are added to
// the run set directly, without a coverage lookup (they're tests, not
// targets).
func verifyBuildResult(db *sql.DB, changedFuncs, changedTestFuncs []query.SymbolRow) (VerifyResult, error) {
	testsByPkg := map[string]map[string]bool{}
	addTest := func(dir, name string) {
		if testsByPkg[dir] == nil {
			testsByPkg[dir] = map[string]bool{}
		}
		testsByPkg[dir][name] = true
	}

	var uncovered []string
	for i := range changedFuncs {
		sym := &changedFuncs[i]
		rows, err := query.FindTests(db, sym.ID, false, verifyTestLimit, 0)
		if err != nil {
			return VerifyResult{}, err
		}
		if len(rows) == 0 {
			uncovered = append(uncovered, verifyQualifiedName(sym))
			continue
		}
		for j := range rows {
			addTest(filepath.Dir(rows[j].FilePathRel), rows[j].Name)
		}
	}
	for i := range changedTestFuncs {
		sym := &changedTestFuncs[i]
		addTest(filepath.Dir(sym.FilePathRel), sym.Name)
	}
	sort.Strings(uncovered)

	dirs := make([]string, 0, len(testsByPkg))
	for d := range testsByPkg {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	var packages []VerifyPackage
	covered := 0
	for _, d := range dirs {
		names := make([]string, 0, len(testsByPkg[d]))
		for n := range testsByPkg[d] {
			names = append(names, n)
		}
		sort.Strings(names)
		covered += len(names)

		pkgPath := "."
		if d != "." && d != "" {
			pkgPath = "./" + d
		}
		packages = append(packages, VerifyPackage{
			Pkg:   pkgPath,
			Run:   fmt.Sprintf("go test %s -run '%s'", pkgPath, strings.Join(names, "|")),
			Tests: names,
		})
	}

	return VerifyResult{
		ChangedSymbols: len(changedFuncs) + len(changedTestFuncs),
		Covered:        covered,
		Uncovered:      uncovered,
		Packages:       packages,
	}, nil
}

// verifyQualifiedName renders a changed symbol as "pkg.Name" (e.g.
// "risk.parseHunkHead") for the uncovered-symbols list — path.Base of the
// full import path, not the file path, so it reads like a Go selector.
func verifyQualifiedName(sym *query.SymbolRow) string {
	pkg := path.Base(sym.PkgPath)
	if pkg == "" || pkg == "." {
		return sym.Name
	}
	return pkg + "." + sym.Name
}

// writeVerify dispatches to the JSON envelope or the terse Claude-default
// text renderer per the global --format flag.
func writeVerify(v VerifyResult, root string, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		return writeVerifyJSON(v, root, start)
	}
	return writeVerifyText(v)
}

// writeVerifyMessage renders a degenerate-path result (empty diff, diff
// unavailable, no callable symbols) — always exit 0, never a gate.
func writeVerifyMessage(root string, start time.Time, msg string) error {
	return writeVerify(VerifyResult{Message: msg}, root, start)
}

// writeVerifyJSON emits the result inside the standard envelope; the single
// result is the sole entry, consumers read `.results[0]` (mirrors writeRiskJSON).
func writeVerifyJSON(v VerifyResult, root string, start time.Time) error {
	resp := output.Response[VerifyResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []VerifyResult{v},
		Meta: output.Meta{
			Command:  cmdNameVerify,
			RepoRoot: root,
			Ms:       time.Since(start).Milliseconds(),
			Total:    1,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func writeVerifyText(v VerifyResult) error {
	var b strings.Builder

	if v.Message != "" {
		fmt.Fprintln(&b, v.Message)
		_, err := os.Stdout.WriteString(b.String())
		return err
	}

	fmt.Fprintf(&b, "%d symbol%s changed · %d test%s cover them\n",
		v.ChangedSymbols, verifyPlural(v.ChangedSymbols), v.Covered, verifyPlural(v.Covered))

	for _, p := range v.Packages {
		fmt.Fprintln(&b, p.Run)
	}

	if len(v.Uncovered) > 0 {
		verb := "has"
		if len(v.Uncovered) != 1 {
			verb = "have"
		}
		fmt.Fprintf(&b, "! %d changed symbol%s %s no covering test: %s\n",
			len(v.Uncovered), verifyPlural(len(v.Uncovered)), verb, strings.Join(v.Uncovered, ", "))
	}

	fmt.Fprintln(&b, verifyClaimNote)

	_, err := os.Stdout.WriteString(b.String())
	return err
}

func verifyPlural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
