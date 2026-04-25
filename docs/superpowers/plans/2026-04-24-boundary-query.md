# `snipe boundary` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `snipe boundary <set-a> <set-b>` command that surfaces every symbol whose refs cross between two sets of packages — the data needed for module-split planning.

**Architecture:** Single SQL query: `refs ⨝ symbols(target on refs.symbol_id) ⨝ symbols(enclosing on refs.enclosing_id)`. Each set is one or more package patterns (exact `internal/store` or recursive `internal/store/...`). A ref is "crossing" iff `enclosing.pkg_path ∈ A` and `target.pkg_path ∈ B` (or vice versa). Default output: per-direction summary grouped by symbol kind, top symbols by ref count. `--detailed` adds per-ref file:line. `--format=json` for orca.

**Tech Stack:** Go, SQLite (via existing `internal/store`), cobra, no new deps. Pattern matching uses SQL `LIKE` for `pkg/...` recursion and exact `=` otherwise — no doublestar dep needed.

---

## File Structure

- **Create:** `internal/query/boundary.go` — `MatchPackagePatterns`, `FindBoundaryCrossings`, types: `BoundaryRef`, `BoundaryReport`
- **Create:** `internal/query/boundary_test.go` — unit tests against an in-memory fixture DB
- **Create:** `cmd/boundary.go` — cobra command, flag parsing, calls into `query`
- **Create:** `test/blackbox/boundary_test.go` — blackbox: index snipe itself, query a known boundary, assert
- **Modify:** `internal/output/types.go` — add `BoundaryResult`, `BoundaryDirection`, `BoundarySymbol`, `BoundaryRefLoc`
- **Modify:** `internal/output/json.go` — register `Response[BoundaryResult]` in `writeClaude`, add `writeClaudeBoundary`

Each file has one job: query layer owns SQL + matching, cmd layer owns flag parsing + I/O, output layer owns rendering, tests are colocated with their layer.

---

### Task 1: Pattern matching helper

The whole query depends on knowing which `pkg_path` strings match a given pattern. Build that primitive first with tight tests so later SQL can compose it confidently.

**Files:**
- Create: `internal/query/boundary.go`
- Create: `internal/query/boundary_test.go`

- [ ] **Step 1: Write failing tests for `MatchPackagePatterns`**

```go
// internal/query/boundary_test.go
package query

import (
	"reflect"
	"sort"
	"testing"
)

func TestMatchPackagePatterns(t *testing.T) {
	universe := []string{
		"github.com/x/proj/internal/store",
		"github.com/x/proj/internal/store/migrations",
		"github.com/x/proj/internal/query",
		"github.com/x/proj/internal/query/resolve",
		"github.com/x/proj/cmd",
	}

	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{
			name:     "exact suffix match — single package",
			patterns: []string{"internal/store"},
			want:     []string{"github.com/x/proj/internal/store"},
		},
		{
			name:     "recursive suffix — package and descendants",
			patterns: []string{"internal/store/..."},
			want: []string{
				"github.com/x/proj/internal/store",
				"github.com/x/proj/internal/store/migrations",
			},
		},
		{
			name:     "multiple patterns union",
			patterns: []string{"internal/store", "cmd"},
			want: []string{
				"github.com/x/proj/cmd",
				"github.com/x/proj/internal/store",
			},
		},
		{
			name:     "no match returns empty",
			patterns: []string{"internal/notreal"},
			want:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchPackagePatterns(universe, tc.patterns)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query -run TestMatchPackagePatterns -v`
Expected: FAIL with "undefined: MatchPackagePatterns"

- [ ] **Step 3: Implement `MatchPackagePatterns`**

```go
// internal/query/boundary.go
package query

import "strings"

// MatchPackagePatterns returns the subset of `universe` matching any pattern.
// A pattern is either an exact suffix (e.g. "internal/store") or a recursive
// suffix ending in "/..." (e.g. "internal/store/..."), which matches the
// package itself and any descendant.
//
// Matching is suffix-based against the full pkg_path so callers can use short
// forms — `internal/store` matches `github.com/x/proj/internal/store`.
func MatchPackagePatterns(universe, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(universe))
	var out []string
	for _, pkg := range universe {
		for _, pat := range patterns {
			if matches(pkg, pat) {
				if _, dup := seen[pkg]; !dup {
					seen[pkg] = struct{}{}
					out = append(out, pkg)
				}
				break
			}
		}
	}
	return out
}

func matches(pkg, pattern string) bool {
	if strings.HasSuffix(pattern, "/...") {
		base := strings.TrimSuffix(pattern, "/...")
		return strings.HasSuffix(pkg, "/"+base) || pkg == base ||
			strings.Contains(pkg, "/"+base+"/")
	}
	return pkg == pattern || strings.HasSuffix(pkg, "/"+pattern)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query -run TestMatchPackagePatterns -v`
Expected: PASS for all four subtests

- [ ] **Step 5: Commit**

```bash
git add internal/query/boundary.go internal/query/boundary_test.go
git commit -m "feat(query): MatchPackagePatterns — exact + recursive package globs"
```

---

### Task 2: SQL query for boundary refs

**Files:**
- Modify: `internal/query/boundary.go`
- Modify: `internal/query/boundary_test.go`

- [ ] **Step 1: Write failing test using a fixture DB**

Add to `internal/query/boundary_test.go`:

```go
import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// seedBoundaryFixture builds a tiny in-memory schema with the columns the
// real index has (subset). Avoids spinning up a real index for unit tests.
func seedBoundaryFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mustExec(t, db, `
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT,
			kind TEXT,
			pkg_path TEXT,
			file_path_rel TEXT,
			line_start INT
		);
		CREATE TABLE refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT,
			enclosing_id TEXT,
			file_path_rel TEXT,
			line INT
		);
	`)

	// Two packages: store, query. Symbols:
	//   store.Save (func, in store)         — enclosed by nothing
	//   store.Get  (func, in store)
	//   query.Run  (func, in query)         — calls store.Save
	//   query.helper (func, in query)       — calls query.Run (intra-pkg)
	mustExec(t, db, `
		INSERT INTO symbols VALUES
			('s_save', 'Save', 'func', 'github.com/x/p/internal/store', 'internal/store/store.go', 10),
			('s_get',  'Get',  'func', 'github.com/x/p/internal/store', 'internal/store/store.go', 30),
			('q_run',  'Run',  'func', 'github.com/x/p/internal/query', 'internal/query/run.go',   5),
			('q_help', 'helper','func','github.com/x/p/internal/query', 'internal/query/run.go',   40);

		INSERT INTO refs VALUES
			('r1', 's_save', 'q_run',  'internal/query/run.go', 12),
			('r2', 's_get',  'q_run',  'internal/query/run.go', 18),
			('r3', 'q_run',  'q_help', 'internal/query/run.go', 45);
	`)
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec: %v\nsql: %s", err, q)
	}
}

func TestFindBoundaryCrossings_StoreToQuery(t *testing.T) {
	db := seedBoundaryFixture(t)
	report, err := FindBoundaryCrossings(db,
		[]string{"github.com/x/p/internal/store"},
		[]string{"github.com/x/p/internal/query"},
	)
	if err != nil {
		t.Fatalf("FindBoundaryCrossings: %v", err)
	}

	// Expect query → store: 2 refs (Save, Get), both called by q_run.
	if got := len(report.BToA); got != 2 {
		t.Errorf("B→A symbols: got %d, want 2", got)
	}
	// Expect store → query: 0 refs.
	if got := len(report.AToB); got != 0 {
		t.Errorf("A→B symbols: got %d, want 0", got)
	}
	// Self-refs (q_run → q_help, both in query) must be excluded.
	for _, s := range report.BToA {
		if s.Symbol == "helper" {
			t.Errorf("intra-set ref leaked: %v", s)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query -run TestFindBoundaryCrossings -v`
Expected: FAIL with "undefined: FindBoundaryCrossings"

- [ ] **Step 3: Implement `FindBoundaryCrossings`**

Add to `internal/query/boundary.go`:

```go
import (
	"database/sql"
	"fmt"
	"strings"
)

// BoundaryRef is one symbol whose refs cross between two sets, plus locations.
type BoundaryRef struct {
	Symbol     string         // name (no receiver prefix)
	Kind       string         // func | method | type | ...
	SourcePkg  string         // package containing the ref site (enclosing)
	TargetPkg  string         // package containing the symbol definition
	RefCount   int            // total cross-boundary refs to this symbol
	Locations  []BoundaryLoc  // detailed per-ref sites; populated when requested
}

// BoundaryLoc is a single ref site (file:line in the SOURCE package).
type BoundaryLoc struct {
	File string
	Line int
}

// BoundaryReport groups crossings by direction. AToB = refs whose enclosing
// is in set A and target is in set B. BToA is the reverse.
type BoundaryReport struct {
	SetA []string // resolved pkg_paths in set A
	SetB []string // resolved pkg_paths in set B
	AToB []BoundaryRef
	BToA []BoundaryRef
}

// FindBoundaryCrossings runs the 3-way join and returns a report.
// setA and setB are full pkg_paths (already resolved via MatchPackagePatterns).
// Locations are not populated — call FindBoundaryLocations for that.
func FindBoundaryCrossings(db *sql.DB, setA, setB []string) (*BoundaryReport, error) {
	if len(setA) == 0 || len(setB) == 0 {
		return &BoundaryReport{SetA: setA, SetB: setB}, nil
	}

	atob, err := queryDirection(db, setA, setB)
	if err != nil {
		return nil, fmt.Errorf("A→B: %w", err)
	}
	btoa, err := queryDirection(db, setB, setA)
	if err != nil {
		return nil, fmt.Errorf("B→A: %w", err)
	}

	return &BoundaryReport{SetA: setA, SetB: setB, AToB: atob, BToA: btoa}, nil
}

// queryDirection finds refs where enclosing.pkg ∈ src AND target.pkg ∈ dst.
// Aggregated by target symbol so repeated call sites become a count.
func queryDirection(db *sql.DB, srcPkgs, dstPkgs []string) ([]BoundaryRef, error) {
	srcPlace := placeholders(len(srcPkgs))
	dstPlace := placeholders(len(dstPkgs))

	q := fmt.Sprintf(`
		SELECT tgt.name, tgt.kind, enc.pkg_path, tgt.pkg_path, COUNT(*) AS n
		FROM refs r
		JOIN symbols tgt ON tgt.id = r.symbol_id
		JOIN symbols enc ON enc.id = r.enclosing_id
		WHERE enc.pkg_path IN (%s)
		  AND tgt.pkg_path IN (%s)
		  AND enc.pkg_path != tgt.pkg_path
		GROUP BY tgt.id
		ORDER BY n DESC, tgt.name ASC
	`, srcPlace, dstPlace)

	args := make([]any, 0, len(srcPkgs)+len(dstPkgs))
	for _, p := range srcPkgs {
		args = append(args, p)
	}
	for _, p := range dstPkgs {
		args = append(args, p)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BoundaryRef
	for rows.Next() {
		var b BoundaryRef
		if err := rows.Scan(&b.Symbol, &b.Kind, &b.SourcePkg, &b.TargetPkg, &b.RefCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query -run TestFindBoundaryCrossings -v`
Expected: PASS — B→A has 2 symbols (Save, Get), A→B has 0, helper excluded.

- [ ] **Step 5: Commit**

```bash
git add internal/query/boundary.go internal/query/boundary_test.go
git commit -m "feat(query): FindBoundaryCrossings — 3-way join for cross-package refs"
```

---

### Task 3: Per-ref location detail

**Files:**
- Modify: `internal/query/boundary.go`
- Modify: `internal/query/boundary_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/query/boundary_test.go`:

```go
func TestFindBoundaryLocations_PopulatesLineFile(t *testing.T) {
	db := seedBoundaryFixture(t)
	report, err := FindBoundaryCrossings(db,
		[]string{"github.com/x/p/internal/store"},
		[]string{"github.com/x/p/internal/query"},
	)
	if err != nil {
		t.Fatalf("FindBoundaryCrossings: %v", err)
	}

	if err := PopulateBoundaryLocations(db, report); err != nil {
		t.Fatalf("PopulateBoundaryLocations: %v", err)
	}

	for _, b := range report.BToA {
		if len(b.Locations) != b.RefCount {
			t.Errorf("%s: got %d locations, want %d", b.Symbol, len(b.Locations), b.RefCount)
		}
		for _, loc := range b.Locations {
			if loc.File == "" || loc.Line == 0 {
				t.Errorf("%s: empty location %+v", b.Symbol, loc)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/query -run TestFindBoundaryLocations -v`
Expected: FAIL with "undefined: PopulateBoundaryLocations"

- [ ] **Step 3: Implement `PopulateBoundaryLocations`**

Add to `internal/query/boundary.go`:

```go
// PopulateBoundaryLocations fills BoundaryRef.Locations for every entry in
// the report — file_path_rel + line for each crossing ref site. Done as a
// separate pass so the summary path stays cheap.
func PopulateBoundaryLocations(db *sql.DB, report *BoundaryReport) error {
	if report == nil {
		return nil
	}
	if err := fillLocations(db, report.AToB, report.SetA, report.SetB); err != nil {
		return fmt.Errorf("A→B: %w", err)
	}
	if err := fillLocations(db, report.BToA, report.SetB, report.SetA); err != nil {
		return fmt.Errorf("B→A: %w", err)
	}
	return nil
}

func fillLocations(db *sql.DB, refs []BoundaryRef, srcPkgs, dstPkgs []string) error {
	if len(refs) == 0 {
		return nil
	}
	srcPlace := placeholders(len(srcPkgs))
	dstPlace := placeholders(len(dstPkgs))

	q := fmt.Sprintf(`
		SELECT tgt.name, r.file_path_rel, r.line
		FROM refs r
		JOIN symbols tgt ON tgt.id = r.symbol_id
		JOIN symbols enc ON enc.id = r.enclosing_id
		WHERE enc.pkg_path IN (%s)
		  AND tgt.pkg_path IN (%s)
		  AND enc.pkg_path != tgt.pkg_path
		ORDER BY r.file_path_rel, r.line
	`, srcPlace, dstPlace)

	args := make([]any, 0, len(srcPkgs)+len(dstPkgs))
	for _, p := range srcPkgs {
		args = append(args, p)
	}
	for _, p := range dstPkgs {
		args = append(args, p)
	}

	rows, err := db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	bySymbol := make(map[string]*BoundaryRef, len(refs))
	for i := range refs {
		bySymbol[refs[i].Symbol] = &refs[i]
	}

	for rows.Next() {
		var name, file string
		var line int
		if err := rows.Scan(&name, &file, &line); err != nil {
			return err
		}
		if br, ok := bySymbol[name]; ok {
			br.Locations = append(br.Locations, BoundaryLoc{File: file, Line: line})
		}
	}
	return rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/query -run TestFindBoundaryLocations -v`
Expected: PASS — Save has 1 location at run.go:12, Get has 1 at run.go:18.

- [ ] **Step 5: Commit**

```bash
git add internal/query/boundary.go internal/query/boundary_test.go
git commit -m "feat(query): PopulateBoundaryLocations — per-ref file:line for --detailed"
```

---

### Task 4: Output types

**Files:**
- Modify: `internal/output/types.go`

- [ ] **Step 1: Add result types**

Append to `internal/output/types.go` (right before the closing of the file is fine; place after `DepTreeEdge` if you want them grouped with deps):

```go
// BoundaryResult is the response for `snipe boundary`. One result per direction.
type BoundaryResult struct {
	SetA       []string             `json:"set_a"`
	SetB       []string             `json:"set_b"`
	Directions []BoundaryDirection  `json:"directions"`
}

// BoundaryDirection is one half of the bidirectional summary.
type BoundaryDirection struct {
	From    string             `json:"from"`     // "A" | "B"
	To      string             `json:"to"`       // "B" | "A"
	Total   int                `json:"total"`    // total ref count across all symbols
	Symbols []BoundarySymbol   `json:"symbols"`
}

// BoundarySymbol is one target symbol referenced across the boundary.
type BoundarySymbol struct {
	Symbol     string         `json:"symbol"`
	Kind       string         `json:"kind"`
	SourcePkg  string         `json:"source_pkg"`
	TargetPkg  string         `json:"target_pkg"`
	RefCount   int            `json:"ref_count"`
	Locations  []BoundaryLoc  `json:"locations,omitempty"` // populated when --detailed
}

// BoundaryLoc is a file:line pair (paths are repo-relative).
type BoundaryLoc struct {
	File string `json:"file"`
	Line int    `json:"line"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/output/types.go
git commit -m "feat(output): BoundaryResult types for snipe boundary command"
```

---

### Task 5: Claude-text writer

**Files:**
- Modify: `internal/output/json.go`

- [ ] **Step 1: Wire dispatcher**

In `internal/output/json.go`, find `writeClaude` (around line 79). Add a case after the `Response[DepTreeResult]` case:

```go
case Response[BoundaryResult]:
    w.writeClaudeBoundary(&b, r.Results, r.Meta)
```

- [ ] **Step 2: Implement the writer**

Add at the end of `internal/output/json.go` (after `writeClaudeDepTree`):

```go
func (w *Writer) writeClaudeBoundary(b *strings.Builder, results []BoundaryResult, meta Meta) {
	for _, r := range results {
		fmt.Fprintf(b, "# boundary: {%s} ↔ {%s}\n",
			strings.Join(r.SetA, ","), strings.Join(r.SetB, ","))

		for _, dir := range r.Directions {
			fmt.Fprintf(b, "%s→%s: %d refs to %d symbols\n",
				dir.From, dir.To, dir.Total, len(dir.Symbols))
			for _, s := range dir.Symbols {
				fmt.Fprintf(b, "  %s.%s [%s] — %d refs\n",
					shortPkg(s.TargetPkg), s.Symbol, s.Kind, s.RefCount)
				for _, loc := range s.Locations {
					fmt.Fprintf(b, "    %s:%d\n", loc.File, loc.Line)
				}
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

// shortPkg returns the last path segment of a Go pkg_path for compact output.
func shortPkg(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/output/json.go
git commit -m "feat(output): Claude-text renderer for boundary results"
```

---

### Task 6: Cobra command + flags

**Files:**
- Create: `cmd/boundary.go`

- [ ] **Step 1: Write the command**

```go
// cmd/boundary.go
package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	boundaryDetailed bool
	boundaryDir      string
)

var boundaryCmd = &cobra.Command{
	Use:     "boundary <pkg-set-a> <pkg-set-b>",
	Short:   "Show symbols whose refs cross between two package sets",
	GroupID: "advanced",
	Long: `Find every symbol whose references cross the boundary between two
package sets — the data needed to plan a module split.

Patterns:
  internal/store          exact: just that package
  internal/store/...      recursive: that package and descendants

Examples:
  snipe boundary internal/store internal/query
  snipe boundary 'internal/store/...' 'internal/query/...'
  snipe boundary --detailed internal/store internal/query
  snipe boundary --direction=a-to-b internal/store internal/query`,
	Args: cobra.ExactArgs(2),
	RunE: runBoundary,
}

func init() {
	boundaryCmd.Flags().BoolVar(&boundaryDetailed, "detailed", false,
		"Include per-ref file:line for every crossing")
	boundaryCmd.Flags().StringVar(&boundaryDir, "direction", "both",
		"both | a-to-b | b-to-a")
	rootCmd.AddCommand(boundaryCmd)
}

func runBoundary(cmd *cobra.Command, args []string) error {
	start := time.Now()
	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, "boundary")
	if err != nil {
		return err
	}
	defer s.Close()

	patternsA := []string{args[0]}
	patternsB := []string{args[1]}

	universe, err := allPkgPaths(s.DB())
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}

	setA := query.MatchPackagePatterns(universe, patternsA)
	setB := query.MatchPackagePatterns(universe, patternsB)

	if len(setA) == 0 || len(setB) == 0 {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrNotFound,
			Message: fmt.Sprintf("no packages matched: A=%d B=%d (patterns: %q %q)",
				len(setA), len(setB), args[0], args[1]),
		})
	}

	report, err := query.FindBoundaryCrossings(s.DB(), setA, setB)
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}

	if boundaryDetailed {
		if err := query.PopulateBoundaryLocations(s.DB(), report); err != nil {
			return w.WriteError("boundary", &output.Error{
				Code: output.ErrInternal, Message: err.Error(),
			})
		}
	}

	result := buildBoundaryResult(report)
	resp := output.Response[output.BoundaryResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.BoundaryResult{result},
		Meta: output.Meta{
			Command:    "boundary",
			Query:      map[string]string{"a": args[0], "b": args[1], "direction": boundaryDir},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      countCrossings(report),
		},
	}
	return w.WriteResponse(resp)
}

func allPkgPaths(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT pkg_path FROM symbols WHERE pkg_path != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func buildBoundaryResult(r *query.BoundaryReport) output.BoundaryResult {
	res := output.BoundaryResult{SetA: r.SetA, SetB: r.SetB}
	if boundaryDir == "both" || boundaryDir == "a-to-b" {
		res.Directions = append(res.Directions, makeDir("A", "B", r.AToB))
	}
	if boundaryDir == "both" || boundaryDir == "b-to-a" {
		res.Directions = append(res.Directions, makeDir("B", "A", r.BToA))
	}
	return res
}

func makeDir(from, to string, refs []query.BoundaryRef) output.BoundaryDirection {
	d := output.BoundaryDirection{From: from, To: to, Symbols: make([]output.BoundarySymbol, len(refs))}
	for i, b := range refs {
		d.Symbols[i] = output.BoundarySymbol{
			Symbol:    b.Symbol,
			Kind:      b.Kind,
			SourcePkg: b.SourcePkg,
			TargetPkg: b.TargetPkg,
			RefCount:  b.RefCount,
			Locations: convertLocs(b.Locations),
		}
		d.Total += b.RefCount
	}
	return d
}

func convertLocs(in []query.BoundaryLoc) []output.BoundaryLoc {
	if len(in) == 0 {
		return nil
	}
	out := make([]output.BoundaryLoc, len(in))
	for i, l := range in {
		out[i] = output.BoundaryLoc{File: l.File, Line: l.Line}
	}
	return out
}

func countCrossings(r *query.BoundaryReport) int {
	n := 0
	for _, b := range r.AToB {
		n += b.RefCount
	}
	for _, b := range r.BToA {
		n += b.RefCount
	}
	return n
}
```

- [ ] **Step 2: Verify it compiles and registers**

Run: `go build ./... && go run ./ boundary --help`
Expected: build succeeds; help text shows usage with `--detailed` and `--direction` flags.

- [ ] **Step 3: Commit**

```bash
git add cmd/boundary.go
git commit -m "feat(cmd): boundary command — module-split planning query"
```

---

### Task 7: Blackbox integration test

The unit tests prove the SQL is right. The blackbox test proves the whole pipeline (index → query → output) works against a real index.

**Files:**
- Create: `test/blackbox/boundary_test.go`

- [ ] **Step 1: Pick a known crossing in snipe itself**

Verify the assertion target is real before writing the test. `cmd/deps.go` calls `query.FindPackageDeps` (cmd → internal/query). Run:

```bash
go build -o /tmp/snipe-bd ./
/tmp/snipe-bd boundary cmd internal/query --format json | jq '.results[0].directions[] | select(.from=="A") | {to, total, syms: (.symbols | length)}'
```

Expected: A→B (cmd → query) shows total > 0, syms > 0.

- [ ] **Step 2: Write the test**

```go
//go:build blackbox

package blackbox

import (
	"strings"
	"testing"
)

func TestBoundary_CmdToQuery(t *testing.T) {
	indexRepo(t, repoRoot)

	stdout, stderr, exitCode := run(t, repoRoot,
		"boundary", "cmd", "internal/query", "--format", "json")
	if exitCode != 0 {
		t.Fatalf("boundary exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := assertEnvelope(t, stdout, "boundary")
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}

	r := requireMap(t, results[0], "results[0]")
	dirs := requireSlice(t, r["directions"], "directions")
	if len(dirs) != 2 {
		t.Fatalf("want 2 directions (both), got %d", len(dirs))
	}

	// A→B (cmd → query) must be non-empty: cmd extensively uses query.
	var aToB map[string]any
	for _, d := range dirs {
		m := requireMap(t, d, "direction")
		if getString(t, m["from"], "from") == "A" {
			aToB = m
			break
		}
	}
	if aToB == nil {
		t.Fatal("missing A→B direction")
	}
	if total := int(getFloat(t, aToB["total"], "total")); total < 5 {
		t.Errorf("A→B total: want ≥5, got %d", total)
	}

	syms := requireSlice(t, aToB["symbols"], "symbols")
	if len(syms) == 0 {
		t.Fatal("A→B has 0 symbols")
	}

	// Spot-check: at least one symbol's target_pkg lives under internal/query.
	sawQueryPkg := false
	for _, s := range syms {
		m := requireMap(t, s, "symbol entry")
		tgt := getString(t, m["target_pkg"], "target_pkg")
		if strings.Contains(tgt, "internal/query") {
			sawQueryPkg = true
			break
		}
	}
	if !sawQueryPkg {
		t.Error("no A→B symbol targets internal/query")
	}
}

func TestBoundary_DetailedAddsLocations(t *testing.T) {
	indexRepo(t, repoRoot)

	stdout, _, exitCode := run(t, repoRoot,
		"boundary", "cmd", "internal/query", "--detailed", "--format", "json")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}

	resp := assertEnvelope(t, stdout, "boundary")
	results := requireSlice(t, resp["results"], "results")
	r := requireMap(t, results[0], "results[0]")
	dirs := requireSlice(t, r["directions"], "directions")

	// Find A→B and check at least one symbol has locations populated.
	for _, d := range dirs {
		m := requireMap(t, d, "direction")
		if getString(t, m["from"], "from") != "A" {
			continue
		}
		syms := requireSlice(t, m["symbols"], "symbols")
		for _, s := range syms {
			sm := requireMap(t, s, "sym")
			if locs, ok := sm["locations"].([]any); ok && len(locs) > 0 {
				return // success
			}
		}
	}
	t.Error("--detailed produced zero locations across all A→B symbols")
}
```

- [ ] **Step 3: Run the blackbox tests**

Run: `go test -tags=blackbox ./test/blackbox -run TestBoundary -v`
Expected: both pass.

- [ ] **Step 4: Commit**

```bash
git add test/blackbox/boundary_test.go
git commit -m "test(blackbox): boundary command — cmd→query crossings + --detailed locations"
```

---

### Task 8: Performance check

The acceptance criteria require <500ms on snipe's own graph.

**Files:**
- (no code change unless perf misses)

- [ ] **Step 1: Measure**

Run from snipe repo root:

```bash
go build -o /tmp/snipe-bd ./
hyperfine --warmup 2 '/tmp/snipe-bd boundary cmd internal/query --format json'
hyperfine --warmup 2 '/tmp/snipe-bd boundary cmd internal/query --detailed --format json'
```

Expected: mean < 500ms for both.

- [ ] **Step 2: If perf misses**

Likely culprit: `pkg_path IN (?,?,...)` with no index on `symbols.pkg_path`. Check:

```bash
sqlite3 .snipe/index.db "EXPLAIN QUERY PLAN
  SELECT tgt.name FROM refs r
  JOIN symbols tgt ON tgt.id = r.symbol_id
  JOIN symbols enc ON enc.id = r.enclosing_id
  WHERE enc.pkg_path IN ('github.com/dkoosis/snipe/cmd')
    AND tgt.pkg_path IN ('github.com/dkoosis/snipe/internal/query');"
```

If you see `SCAN symbols`, add an index in a new migration in `internal/store/schema.go`:

```go
CREATE INDEX IF NOT EXISTS idx_symbols_pkg_path ON symbols(pkg_path);
```

Re-run hyperfine. If still slow, document in commit message and bead.

- [ ] **Step 3: Commit (only if changes were needed)**

```bash
git add internal/store/schema.go
git commit -m "perf(store): index symbols.pkg_path for boundary queries"
```

---

### Task 9: Wrap — `make audit`, close bead

**Files:**
- Modify: `.claude/rules/boot.md` (final step)

- [ ] **Step 1: Run the merge gate**

Run: `make audit`
Expected: `=== audit pass ===` at the end.

- [ ] **Step 2: Update boot.md**

Replace the "do next" line and bump SHA after the final commit.

- [ ] **Step 3: Close the bead and push**

```bash
bd close snipe-zhs.4 --reason="boundary command shipped: docs/superpowers/plans/2026-04-24-boundary-query.md"
git add .claude/rules/boot.md
git commit -m "chore: wrap session — boundary query (zhs.4) shipped"
git push
```

---

## Self-Review

**Spec coverage:**
- ✅ "two package sets (glob or list)" — Task 1 (`MatchPackagePatterns`), Task 6 wires args
- ✅ "symbols whose refs cross the boundary" — Task 2 (3-way join, `enc.pkg != tgt.pkg`)
- ✅ "Output: symbol, kind, source pkg, target pkg, ref location" — Task 4 types, Task 3 locations
- ✅ "<500ms on snipe's own graph" — Task 8 measures, fixes if needed
- ✅ "Blackbox test on a fixture with a known cross-boundary ref" — Task 7
- ✅ Direction flag (`a-to-b|b-to-a|both`) — Task 6
- ✅ `--detailed` per-ref locations — Task 3 query, Task 6 wires flag, Task 5 renders, Task 7 asserts
- ✅ `--format=json` works (output already supports it via the existing writer)
- ⚠️  `--exclude-tests` (default true) — **dropped from v1**: tests reference production code constantly; if dk wants this, file as a follow-up bead. Documented here for visibility.
- ⚠️  Symbol-kind grouping in summary — Task 5 prints kind per-row, doesn't aggregate counts by kind. The summary line ("X→Y: N refs to M symbols") is enough for v1; per-kind subtotals can be a follow-up.

**Placeholder scan:** None.

**Type consistency:** `BoundaryRef` (query layer) vs `BoundarySymbol` (output layer) are intentionally distinct — query owns DB-shaped data, output owns wire format. `BoundaryLoc` exists in both packages with identical fields (`File`, `Line`); Task 6's `convertLocs` bridges them. Renaming the query type would also be fine.

---

**Plan complete and saved to `docs/superpowers/plans/2026-04-24-boundary-query.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
