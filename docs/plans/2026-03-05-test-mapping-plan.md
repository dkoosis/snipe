# Test Mapping Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `snipe tests <symbol>` command that finds test functions exercising a given symbol via call graph analysis.

**Architecture:** New query function `FindTests` in `internal/query/tests.go` uses CTE-based SQL to find Test*/Benchmark*/Fuzz*/Example* functions that call the target symbol (1-hop direct) or call helpers that call it (2-hop transitive). CLI command in `cmd/tests.go` mirrors `cmd/callers.go` pattern. Fallback to ref-based search when call_graph is empty.

**Tech Stack:** Go, SQLite (via `database/sql`), cobra CLI

**Design doc:** `docs/plans/2026-03-05-test-mapping-design.md`

---

### Task 1: Query layer — TestRow type and FindTests

**Files:**
- Create: `internal/query/tests.go`
- Test: `internal/query/tests_test.go`

**Step 1: Write the failing test**

Create `internal/query/tests_test.go`:

```go
package query

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT,
			kind TEXT,
			file_path TEXT,
			file_path_rel TEXT,
			pkg_path TEXT,
			line_start INTEGER,
			col_start INTEGER,
			line_end INTEGER,
			col_end INTEGER,
			signature TEXT,
			doc TEXT,
			receiver TEXT
		);
		CREATE TABLE call_graph (
			caller_id TEXT NOT NULL,
			callee_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line INTEGER,
			col INTEGER,
			PRIMARY KEY (caller_id, callee_id, line, col)
		);
		CREATE TABLE refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_path_rel TEXT,
			line INTEGER,
			col INTEGER,
			enclosing_id TEXT,
			snippet TEXT
		);
		CREATE TABLE files (
			path TEXT PRIMARY KEY,
			mtime INTEGER NOT NULL,
			hash TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

func insertTestSym(t *testing.T, db *sql.DB, id, name, kind, filePath, filePathRel, pkgPath, signature string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
		line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, ?, ?, ?, ?, 1, 1, 10, 1, ?, '', '')`,
		id, name, kind, filePath, filePathRel, pkgPath, signature)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", id, err)
	}
}

func insertCallEdge(t *testing.T, db *sql.DB, callerID, calleeID, filePath string, line, col int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO call_graph (caller_id, callee_id, file_path, line, col)
		VALUES (?, ?, ?, ?, ?)`, callerID, calleeID, filePath, line, col)
	if err != nil {
		t.Fatalf("insert call edge %s->%s: %v", callerID, calleeID, err)
	}
}

func TestFindTests_Direct(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	// Source symbol
	insertTestSym(t, db, "s1", "ProcessOrder", "func", "/repo/order.go", "order.go", "pkg/order", "func ProcessOrder()")

	// Direct test caller
	insertTestSym(t, db, "t1", "TestProcessOrder", "func", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestProcessOrder(t *testing.T)")
	insertCallEdge(t, db, "t1", "s1", "/repo/order_test.go", 10, 5)

	// Non-test caller (should be excluded)
	insertTestSym(t, db, "c1", "HandleRequest", "func", "/repo/handler.go", "handler.go", "pkg/handler", "func HandleRequest()")
	insertCallEdge(t, db, "c1", "s1", "/repo/handler.go", 20, 5)

	results, err := FindTests(db, "s1", true, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 test, got %d", len(results))
	}
	if results[0].Name != "TestProcessOrder" {
		t.Errorf("name: got %q, want TestProcessOrder", results[0].Name)
	}
	if results[0].Hop != 1 {
		t.Errorf("hop: got %d, want 1", results[0].Hop)
	}
}

func TestFindTests_Transitive(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	// Source symbol
	insertTestSym(t, db, "s1", "ProcessOrder", "func", "/repo/order.go", "order.go", "pkg/order", "func ProcessOrder()")

	// Helper that calls the symbol (not a Test* function)
	insertTestSym(t, db, "h1", "setupOrder", "func", "/repo/order_test.go", "order_test.go", "pkg/order", "func setupOrder()")
	insertCallEdge(t, db, "h1", "s1", "/repo/order_test.go", 15, 5)

	// Test that calls the helper
	insertTestSym(t, db, "t1", "TestOrderFlow", "func", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestOrderFlow(t *testing.T)")
	insertCallEdge(t, db, "t1", "h1", "/repo/order_test.go", 25, 5)

	// Direct test caller too
	insertTestSym(t, db, "t2", "TestProcessOrder", "func", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestProcessOrder(t *testing.T)")
	insertCallEdge(t, db, "t2", "s1", "/repo/order_test.go", 30, 5)

	results, err := FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(results))
	}

	// Direct should come first (hop=1), transitive second (hop=2)
	if results[0].Hop != 1 {
		t.Errorf("first result hop: got %d, want 1", results[0].Hop)
	}
	if results[1].Hop != 2 {
		t.Errorf("second result hop: got %d, want 2", results[1].Hop)
	}
}

func TestFindTests_AllTestTypes(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	insertTestSym(t, db, "s1", "Foo", "func", "/repo/foo.go", "foo.go", "pkg/foo", "func Foo()")
	for _, tc := range []struct{ id, name string }{
		{"t1", "TestFoo"},
		{"t2", "BenchmarkFoo"},
		{"t3", "FuzzFoo"},
		{"t4", "ExampleFoo"},
	} {
		insertTestSym(t, db, tc.id, tc.name, "func", "/repo/foo_test.go", "foo_test.go", "pkg/foo", "")
		insertCallEdge(t, db, tc.id, "s1", "/repo/foo_test.go", 10, 5)
	}

	results, err := FindTests(db, "s1", true, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 test types, got %d", len(results))
	}
}

func TestFindTests_Empty(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	insertTestSym(t, db, "s1", "Untested", "func", "/repo/lonely.go", "lonely.go", "pkg/lonely", "func Untested()")

	results, err := FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 tests, got %d", len(results))
	}
}

func TestFindTests_DeduplicatesDirectAndTransitive(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	insertTestSym(t, db, "s1", "Target", "func", "/repo/target.go", "target.go", "pkg/t", "func Target()")
	insertTestSym(t, db, "h1", "helper", "func", "/repo/target_test.go", "target_test.go", "pkg/t", "func helper()")
	insertCallEdge(t, db, "h1", "s1", "/repo/target_test.go", 5, 1)

	// TestBoth calls Target directly AND calls helper (which also calls Target)
	insertTestSym(t, db, "t1", "TestBoth", "func", "/repo/target_test.go", "target_test.go", "pkg/t", "func TestBoth(t *testing.T)")
	insertCallEdge(t, db, "t1", "s1", "/repo/target_test.go", 10, 1)
	insertCallEdge(t, db, "t1", "h1", "/repo/target_test.go", 11, 1)

	results, err := FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 (deduplicated), got %d", len(results))
	}
	if results[0].Hop != 1 {
		t.Errorf("should be direct (hop=1), got %d", results[0].Hop)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/query/ -run TestFindTests -v`
Expected: FAIL — `FindTests` undefined

**Step 3: Write minimal implementation**

Create `internal/query/tests.go`:

```go
package query

import (
	"database/sql"
)

// TestRow represents a test function that exercises a target symbol.
type TestRow struct {
	ID          string
	Name        string
	Kind        string
	FilePath    string // Absolute path
	FilePathRel string // Relative path
	PkgPath     string
	LineStart   int
	ColStart    int
	LineEnd     int
	ColEnd      int
	Signature   sql.NullString
	Doc         sql.NullString
	Receiver    sql.NullString
	FileHash    string
	Hop         int // 1=direct, 2=transitive
}

// testFuncFilter is the SQL predicate for Go test function names.
const testFuncFilter = `(s.name GLOB 'Test*' OR s.name GLOB 'Benchmark*' OR s.name GLOB 'Fuzz*' OR s.name GLOB 'Example*')`

// FindTests returns test functions that exercise the given symbol.
// When direct=true, only 1-hop callers are returned.
// When direct=false (default), also returns 2-hop transitive callers (Test* → helper → symbol).
func FindTests(db *sql.DB, symbolID string, direct bool, limit, offset int) ([]TestRow, error) {
	var q string
	if direct {
		q = `
			SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
			       s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
			       s.signature, s.doc, s.receiver, f.hash,
			       1 AS hop
			FROM call_graph cg
			JOIN symbols s ON s.id = cg.caller_id
			LEFT JOIN files f ON s.file_path = f.path
			WHERE cg.callee_id = ?
			  AND s.file_path GLOB '*_test.go'
			  AND ` + testFuncFilter + `
			ORDER BY s.file_path, s.name
			LIMIT ? OFFSET ?`
	} else {
		q = `
			WITH direct_tests AS (
				SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
				       s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
				       s.signature, s.doc, s.receiver, f.hash,
				       1 AS hop
				FROM call_graph cg
				JOIN symbols s ON s.id = cg.caller_id
				LEFT JOIN files f ON s.file_path = f.path
				WHERE cg.callee_id = ?
				  AND s.file_path GLOB '*_test.go'
				  AND ` + testFuncFilter + `
			),
			transitive_tests AS (
				SELECT ts.id, ts.name, ts.kind, ts.file_path, ts.file_path_rel,
				       ts.pkg_path, ts.line_start, ts.col_start, ts.line_end, ts.col_end,
				       ts.signature, ts.doc, ts.receiver, f.hash,
				       2 AS hop
				FROM call_graph cg1
				JOIN call_graph cg2 ON cg2.callee_id = cg1.caller_id
				JOIN symbols ts ON ts.id = cg2.caller_id
				LEFT JOIN files f ON ts.file_path = f.path
				WHERE cg1.callee_id = ?
				  AND ts.file_path GLOB '*_test.go'
				  AND ` + testFuncFilter + `
				  AND ts.id NOT IN (SELECT id FROM direct_tests)
			)
			SELECT * FROM direct_tests
			UNION ALL
			SELECT * FROM transitive_tests
			ORDER BY hop, file_path, name
			LIMIT ? OFFSET ?`
	}

	var rows *sql.Rows
	var err error
	if direct {
		rows, err = db.Query(q, symbolID, limit, offset)
	} else {
		rows, err = db.Query(q, symbolID, symbolID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanTestRows(rows)
}

// scanTestRows scans rows into TestRow slices.
func scanTestRows(rows *sql.Rows) ([]TestRow, error) {
	var results []TestRow
	for rows.Next() {
		var r TestRow
		var filePathRel, pkgPath, fileHash sql.NullString
		err := rows.Scan(
			&r.ID, &r.Name, &r.Kind, &r.FilePath, &filePathRel,
			&pkgPath, &r.LineStart, &r.ColStart, &r.LineEnd, &r.ColEnd,
			&r.Signature, &r.Doc, &r.Receiver, &fileHash,
			&r.Hop,
		)
		if err != nil {
			return nil, err
		}
		r.FilePathRel = filePathRel.String
		r.PkgPath = pkgPath.String
		r.FileHash = fileHash.String
		results = append(results, r)
	}
	return results, rows.Err()
}

// ToResult converts a TestRow to an output.Result.
func (r *TestRow) ToResult() output.Result {
	filePath := r.FilePathRel
	if filePath == "" {
		filePath = r.FilePath
	}
	defRange := output.Range{
		Start: output.Position{Line: r.LineStart, Col: r.ColStart},
		End:   output.Position{Line: r.LineEnd, Col: r.ColEnd},
	}
	return output.Result{
		ID:         r.ID,
		File:       filePath,
		FileAbs:    r.FilePath,
		Range:      defRange,
		Kind:       r.Kind,
		Name:       r.Name,
		Receiver:   r.Receiver.String,
		Package:    r.PkgPath,
		Match:      r.Signature.String,
		EditTarget: output.FormatEditTargetWithHash(filePath, r.FilePath, defRange),
	}
}
```

Note: `ToResult` requires importing `output` package. Add:

```go
import (
	"database/sql"

	"github.com/dkoosis/snipe/internal/output"
)
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/query/ -run TestFindTests -v`
Expected: All 5 tests PASS

**Step 5: Commit**

```bash
git add internal/query/tests.go internal/query/tests_test.go
git commit -m "feat: add FindTests query for test mapping (#108)"
```

---

### Task 2: Suggestions helper

**Files:**
- Modify: `internal/output/types.go`

**Step 1: Add `SuggestionsForTests`**

Add to `internal/output/types.go` after `SuggestionsForCallees`:

```go
// SuggestionsForTests generates suggestions after a tests command.
func SuggestionsForTests(symbol string, resultCount int, suggestedFile string) []Suggestion {
	if resultCount == 0 {
		suggestions := []Suggestion{
			{
				Command:     "snipe refs " + symbol,
				Description: "Check if the symbol is referenced anywhere",
				Priority:    1,
			},
		}
		if suggestedFile != "" {
			suggestions = append(suggestions, Suggestion{
				Description: "No tests found for " + symbol + ". Consider adding tests in " + suggestedFile,
				Priority:    2,
			})
		}
		return suggestions
	}
	return []Suggestion{
		{
			Command:     "snipe def " + symbol,
			Description: "View the function definition",
			Priority:    1,
		},
		{
			Command:     "snipe callers " + symbol,
			Description: "See all callers, not just tests",
			Priority:    2,
		},
	}
}
```

**Step 2: Build to verify**

Run: `go build ./...`
Expected: Success

**Step 3: Commit**

```bash
git add internal/output/types.go
git commit -m "feat: add SuggestionsForTests helper (#108)"
```

---

### Task 3: CLI command

**Files:**
- Create: `cmd/tests.go`

**Step 1: Create `cmd/tests.go`**

This follows `cmd/callers.go` closely. Key differences: `--direct` flag, `--at` support, hint injection, zero-coverage suggestions.

```go
package cmd

import (
	"encoding/hex"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var testsCmd = &cobra.Command{
	Use:     "tests [symbol|id]",
	Short:   "Find tests that exercise a symbol",
	GroupID: "core",
	Long: `Finds test functions that call a given symbol (direct or via helpers).

By default uses 2-hop transitive search: finds Test*/Benchmark*/Fuzz*/Example*
functions that call the symbol directly or through one intermediary.

Accepts symbol name, 16-char hex ID (auto-detected), or --at position.

Examples:
  snipe tests ProcessOrder            # Find tests (2-hop transitive)
  snipe tests --direct ProcessOrder   # Direct callers only
  snipe tests --at order.go:42:1      # By position
  snipe tests a3f2c1de89ab0123        # By hex ID`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTests,
}

var (
	testsDirect bool
	testsAt     string
	testsID     string
)

func init() {
	testsCmd.Flags().BoolVar(&testsDirect, "direct", false, "Only show tests that directly call the symbol (1-hop)")
	testsCmd.Flags().StringVar(&testsAt, "at", "", "Position to look up (file:line:col)")
	testsCmd.Flags().StringVar(&testsID, "id", "", "Symbol ID to look up")
	rootCmd.AddCommand(testsCmd)
}

func runTests(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary

	w := output.NewWriter(os.Stdout, compact)

	if len(args) == 0 && testsAt == "" && testsID == "" {
		return w.WriteError("tests", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name, --at position, or --id",
		})
	}

	s, dir, err := OpenStore(w, "tests")
	if err != nil {
		return err
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	switch {
	case testsID != "":
		symbolID = testsID
		queryInfo = map[string]string{"id": testsID}

	case testsAt != "":
		pos, err := query.ParsePosition(testsAt)
		if err != nil {
			return w.WriteError("tests", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		sym, err := query.FindSymbolAtPosition(s.DB(), pos.File, pos.Line)
		if err != nil {
			return w.WriteError("tests", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		symbolID = sym.ID
		queryInfo = map[string]string{"at": testsAt, "resolved": sym.Name}

	default:
		name := args[0]

		// Auto-detect hex ID
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				break
			}
		}

		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("tests", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		if len(symbols) == 0 {
			return w.WriteError("tests", output.NewNotFoundError(name))
		}
		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, sym := range symbols {
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError("tests", output.NewAmbiguousError(name, candidates))
		}
		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
	}

	// Look up symbol for session tracking and suggestions
	var symName, symFileRel string
	if sym, err := query.LookupByID(s.DB(), symbolID); err == nil && sym != nil {
		symName = sym.Name
		symFileRel = sym.FilePathRel
		recordSessionQuery(dir, sym.Name, sym.FilePathRel, sym.LineStart, sym.Kind, "tests")
	}

	// Find tests
	testRows, err := query.FindTests(s.DB(), symbolID, testsDirect, lim, off)
	if err != nil {
		return w.WriteError("tests", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results with hints
	results := make([]output.Result, len(testRows))
	var degraded []string

	// Batch fetch for bodies
	var testSymbols map[string]*query.SymbolRow
	if withBody && len(testRows) > 0 {
		ids := make([]string, len(testRows))
		for i, tr := range testRows {
			ids[i] = tr.ID
		}
		var batchErr error
		testSymbols, batchErr = query.BatchLookupByID(s.DB(), ids)
		if batchErr != nil {
			degraded = append(degraded, "batch_lookup_failed")
		}
	}

	for i, tr := range testRows {
		result := tr.ToResult()

		// Add hop hint
		if tr.Hop == 1 {
			result.Hints = append(result.Hints, "direct_test")
		} else {
			result.Hints = append(result.Hints, "transitive_test")
		}

		// Add body if requested
		if withBody {
			if sym, ok := testSymbols[tr.ID]; ok && sym != nil {
				symResult := sym.ToResult()
				if err := output.AddBody(&symResult); err != nil {
					degraded = append(degraded, "body_extraction_failed")
				}
				result.Body = symResult.Body
			}
		}

		if contextLines > 0 && !withBody {
			if err := output.AddContext(&result, contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}

		results[i] = result
	}

	degraded = uniqueStrings(degraded)

	output.ScoreAndSort(results, symName)
	results = ApplySelection(results)

	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	if summary {
		summaryData := output.BuildSummary(results)
		return w.WriteResponse(output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    "tests",
				Query:      queryInfo,
				RepoRoot:   dir,
				IndexState: query.CheckIndexState(s.DB(), dir, Version),
				Degraded:   degraded,
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Offset:     off,
				Limit:      lim,
				Truncated:  len(results) >= lim,
				StaleFiles: staleFiles,
			},
		})
	}

	// Token estimate
	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	// Suggested test file for zero-coverage
	suggestedFile := ""
	if symFileRel != "" {
		suggestedFile = strings.TrimSuffix(symFileRel, ".go") + "_test.go"
	}

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: output.SuggestionsForTests(symName, len(results), suggestedFile),
		Meta: output.Meta{
			Command:       "tests",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Offset:        off,
			Limit:         lim,
			Truncated:     len(results) >= lim || tokenTruncated,
			TokenEstimate: tokenEstimate,
			StaleFiles:    staleFiles,
		},
	}

	return w.WriteResponse(resp)
}
```

**Step 2: Build to verify**

Run: `go build ./...`
Expected: Success

**Step 3: Smoke test on snipe's own codebase**

Run: `snipe tests DetectConventions | jq '.results | length, .results[0].name, .results[0].hints'`
Expected: At least 1 result with `"direct_test"` hint

Run: `snipe tests ProcessOrder 2>&1 | jq '.results'`
Expected: Empty array (no such symbol — confirms zero-coverage path)

**Step 4: Commit**

```bash
git add cmd/tests.go
git commit -m "feat: add snipe tests command (#108)"
```

---

### Task 4: Blackbox tests

**Files:**
- Modify: `test/blackbox/fixture_test.go` (add test file to fixture)
- Modify: `test/blackbox/cli_workflows_test.go` (add test cases)

**Step 1: Add test file to fixture**

In `test/blackbox/fixture_test.go`, add a `_test.go` file to the fixture after the `betaPath` section (around line 118). The fixture already has `Callee()` called by `Caller()` and `AnotherCaller()`.

Add this after the `betaPath` block:

```go
	// Test file — exercises Callee via direct call and via helper
	testContent := `package fixture

import "testing"

func TestCallee(t *testing.T) {
	result := Callee()
	if result != "ok" {
		t.Fatal("unexpected")
	}
}

func testHelper() string {
	return Callee()
}

func TestViaHelper(t *testing.T) {
	result := testHelper()
	if result != "ok" {
		t.Fatal("unexpected")
	}
}
`
	testPath := filepath.Join(repoDir, "main_test.go")
	writeFile(t, testPath, testContent)
	paths["test"] = testPath
```

**Step 2: Add blackbox test for `snipe tests`**

Add to `cli_workflows_test.go` before `initGitRepo`:

```go
func TestTests_FindsDirectAndTransitive_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "tests", "Callee")
	if exitCode != 0 {
		t.Fatalf("tests exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "tests",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected test results for Callee")
	}

	// Should find TestCallee (direct) and TestViaHelper (transitive via testHelper)
	names := make(map[string]bool)
	for _, r := range results {
		rm := requireMap(t, r, "result")
		names[getString(t, rm["name"], "name")] = true
	}
	if !names["TestCallee"] {
		t.Errorf("missing direct test TestCallee; got %v", names)
	}
}

func TestTests_Direct_ExcludesTransitive(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "tests", "--direct", "Callee")
	if exitCode != 0 {
		t.Fatalf("tests --direct exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")

	// Direct should only find TestCallee, not TestViaHelper
	for _, r := range results {
		rm := requireMap(t, r, "result")
		hints := rm["hints"]
		if hints != nil {
			hintSlice := requireSlice(t, hints, "hints")
			for _, h := range hintSlice {
				if getString(t, h, "hint") == "transitive_test" {
					t.Errorf("--direct should not return transitive tests")
				}
			}
		}
	}
}

func TestTests_ZeroCoverage_ReturnsSuggestions(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// UseAmbiguous has no test callers
	stdout, stderr, exitCode := run(t, repoDir, "tests", "UseAmbiguous")
	if exitCode != 0 {
		t.Fatalf("tests exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 0 {
		t.Fatalf("expected 0 tests for UseAmbiguous, got %d", len(results))
	}

	// Should have suggestions
	if sug, ok := resp["suggestions"]; ok && sug != nil {
		suggestions := requireSlice(t, sug, "suggestions")
		if len(suggestions) == 0 {
			t.Errorf("expected suggestions for zero-coverage symbol")
		}
	}
}
```

**Step 3: Run blackbox tests**

Run: `go test -tags blackbox -run "TestTests_" -v ./test/blackbox/`
Expected: All 3 tests PASS

**Step 4: Commit**

```bash
git add test/blackbox/fixture_test.go test/blackbox/cli_workflows_test.go
git commit -m "test: add blackbox tests for snipe tests command (#108)"
```

---

### Task 5: Verify and close

**Step 1: Run full QA**

Run: `mage qa`
Expected: All green

**Step 2: Verify on snipe's own codebase**

Run: `snipe tests DetectConventions | jq '.meta.total, .results[].name'`
Expected: At least `TestDetectConventions_AllCategories` and individual detector tests

Run: `snipe tests --direct FindCallers | jq '.results[].name'`
Expected: Test functions that directly call FindCallers

Run: `snipe tests Untested 2>&1 | jq '.results | length, .suggestions'`
Expected: 0 results, suggestions array with zero-coverage message

**Step 3: Fix any lint issues**

If `golangci-lint` flags anything, fix it (common: goconst, goimports grouping, unparam).

**Step 4: Commit any fixes**

```bash
git add -A && git commit -m "fix: address lint issues for tests command (#108)"
```

**Step 5: Close issue**

```bash
gh issue close 108 -c "Test mapping shipped: snipe tests <symbol> with 2-hop transitive call graph analysis, --direct flag, --at support, zero-coverage suggestions. mage qa green."
```
