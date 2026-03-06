# snipe impact — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `snipe impact <symbol>` command that returns blast radius (transitive callers, implementers, tests) in one call.

**Architecture:** Three query phases merged into flat `Response[Result]` with hint-based classification. Reuses `FindTests()` for phase 3, adapts the tests CTE pattern for transitive callers in phase 1, calls `FindImplementers()` for phase 2.

**Tech Stack:** Go, SQLite CTEs, cobra CLI

---

### Task 1: Query layer — `FindImpactCallers`

**Files:**
- Create: `internal/query/impact.go`
- Test: `internal/query/impact_test.go`

**Step 1: Write the failing test**

```go
// internal/query/impact_test.go
package query

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupImpactTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Minimal schema
	for _, stmt := range []string{
		`CREATE TABLE symbols (
			id TEXT PRIMARY KEY, name TEXT, kind TEXT,
			file_path TEXT, file_path_rel TEXT, pkg_path TEXT,
			line_start INT, col_start INT, line_end INT, col_end INT,
			signature TEXT, doc TEXT, receiver TEXT
		)`,
		`CREATE TABLE call_graph (
			caller_id TEXT, callee_id TEXT, file_path TEXT, line INT, col INT,
			PRIMARY KEY (caller_id, callee_id, file_path, line, col)
		)`,
		`CREATE TABLE files (path TEXT PRIMARY KEY, mtime INT, hash TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// execOrFatal wraps db.Exec to fail the test on error.
// Prevents silent failures when INSERT statements have wrong column counts.
func execOrFatal(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestFindImpactCallers_Direct(t *testing.T) {
	db := setupImpactTestDB(t)

	// target: funcA, caller: funcB calls funcA
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)

	rows, err := FindImpactCallers(db, "aaa", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 caller, got %d", len(rows))
	}
	if rows[0].ID != "bbb" {
		t.Errorf("expected caller bbb, got %s", rows[0].ID)
	}
	if rows[0].Hop != 1 {
		t.Errorf("expected hop 1, got %d", rows[0].Hop)
	}
}

func TestFindImpactCallers_Transitive(t *testing.T) {
	db := setupImpactTestDB(t)

	// funcC -> funcB -> funcA (target)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('ccc','funcC','func','/h.go','h.go','pkg',20,1,25,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/h.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('ccc','bbb','/h.go',22,5)`)

	rows, err := FindImpactCallers(db, "aaa", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 callers, got %d", len(rows))
	}

	// Verify hop-1 comes first, hop-2 second
	if rows[0].Hop != 1 || rows[0].ID != "bbb" {
		t.Errorf("first result should be direct caller bbb hop=1, got %s hop=%d", rows[0].ID, rows[0].Hop)
	}
	if rows[1].Hop != 2 || rows[1].ID != "ccc" {
		t.Errorf("second result should be transitive caller ccc hop=2, got %s hop=%d", rows[1].ID, rows[1].Hop)
	}
}

func TestFindImpactCallers_DirectOnly(t *testing.T) {
	db := setupImpactTestDB(t)

	// Same setup as transitive test
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('ccc','funcC','func','/h.go','h.go','pkg',20,1,25,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/h.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('ccc','bbb','/h.go',22,5)`)

	rows, err := FindImpactCallers(db, "aaa", true, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 direct caller, got %d", len(rows))
	}
	if rows[0].ID != "bbb" {
		t.Errorf("expected bbb, got %s", rows[0].ID)
	}
}

func TestFindImpactCallers_DedupsTransitive(t *testing.T) {
	db := setupImpactTestDB(t)

	// funcB calls funcA directly AND via funcD
	// funcB should only appear once (as direct, hop=1)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('ddd','funcD','func','/d.go','d.go','pkg',30,1,35,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/d.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)   // direct
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('ddd','aaa','/d.go',32,5)`)   // direct
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','ddd','/g.go',13,5)`)   // bbb -> ddd -> aaa

	rows, err := FindImpactCallers(db, "aaa", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}

	// bbb and ddd are direct (hop 1), bbb also transitive but deduped
	ids := map[string]int{}
	for _, r := range rows {
		ids[r.ID] = r.Hop
	}
	if ids["bbb"] != 1 {
		t.Errorf("bbb should be hop 1 (direct), got %d", ids["bbb"])
	}
	if ids["ddd"] != 1 {
		t.Errorf("ddd should be hop 1 (direct), got %d", ids["ddd"])
	}
	// bbb should NOT appear twice
	count := 0
	for _, r := range rows {
		if r.ID == "bbb" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("bbb should appear exactly once, got %d", count)
	}
}

func TestFindImpactCallers_ExcludesTestFiles(t *testing.T) {
	db := setupImpactTestDB(t)

	// funcB (non-test) and TestFoo (test file) both call funcA
	// Only funcB should appear — test files are handled by FindTests
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('ttt','TestFoo','func','/f_test.go','f_test.go','pkg',1,1,10,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f_test.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('ttt','aaa','/f_test.go',3,5)`)

	rows, err := FindImpactCallers(db, "aaa", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 caller (test files excluded), got %d", len(rows))
	}
	if rows[0].ID != "bbb" {
		t.Errorf("expected bbb, got %s", rows[0].ID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/query/ -run TestFindImpactCallers -v`
Expected: FAIL — `FindImpactCallers` not defined

**Step 3: Write `FindImpactCallers`**

```go
// internal/query/impact.go
package query

import (
	"database/sql"
)

// ImpactRow represents a symbol in the impact blast radius.
// Reuses the same fields as TestRow — same scan pattern (15 columns including hop).
type ImpactRow = TestRow

// FindImpactCallers returns non-test callers of a symbol with hop distance.
// When direct=true, only 1-hop callers. Otherwise includes 2-hop transitive.
// Test files (*_test.go) are excluded — use FindTests() for test coverage.
func FindImpactCallers(db *sql.DB, symbolID string, direct bool, limit, offset int) ([]ImpactRow, error) {
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
			  AND s.file_path NOT GLOB '*_test.go'
			ORDER BY s.file_path, s.name
			LIMIT ? OFFSET ?`
	} else {
		q = `
			WITH direct_callers AS (
				SELECT s.id, s.name, s.kind, s.file_path, s.file_path_rel,
				       s.pkg_path, s.line_start, s.col_start, s.line_end, s.col_end,
				       s.signature, s.doc, s.receiver, f.hash,
				       1 AS hop
				FROM call_graph cg
				JOIN symbols s ON s.id = cg.caller_id
				LEFT JOIN files f ON s.file_path = f.path
				WHERE cg.callee_id = ?
				  AND s.file_path NOT GLOB '*_test.go'
			),
			transitive_callers AS (
				SELECT ts.id, ts.name, ts.kind, ts.file_path, ts.file_path_rel,
				       ts.pkg_path, ts.line_start, ts.col_start, ts.line_end, ts.col_end,
				       ts.signature, ts.doc, ts.receiver, f.hash,
				       2 AS hop
				FROM call_graph cg1
				JOIN call_graph cg2 ON cg2.callee_id = cg1.caller_id
				JOIN symbols ts ON ts.id = cg2.caller_id
				LEFT JOIN files f ON ts.file_path = f.path
				WHERE cg1.callee_id = ?
				  AND ts.file_path NOT GLOB '*_test.go'
				  AND ts.id NOT IN (SELECT id FROM direct_callers)
			)
			SELECT * FROM direct_callers
			UNION ALL
			SELECT * FROM transitive_callers
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
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/query/ -run TestFindImpactCallers -v`
Expected: PASS (all 5 tests)

**Step 5: Commit**

```
feat: add FindImpactCallers query for transitive blast radius (#110)
```

---

### Task 2: Command layer — `cmd/impact.go`

**Files:**
- Create: `cmd/impact.go`
- Modify: `cmd/root.go` (add `"impact": true` to knownSubcommands)
- Modify: `internal/output/types.go` (add hint constants + `SuggestionsForImpact`)

**Step 1: Add hint constants and suggestion helper**

Add to `internal/output/types.go` after existing hint constants:

```go
// Impact hint constants
const (
	HintDirectCaller     = "direct_caller"
	HintTransitiveCaller = "transitive_caller"
	HintImplementer      = "implementer"
	HintDirectTest       = "direct_test"
	HintTransitiveTest   = "transitive_test"
)

// SuggestionsForImpact generates suggestions after an impact command.
func SuggestionsForImpact(symbol string, directCallers, transitiveCallers, implementers, tests, pkgCount int) []Suggestion {
	var suggestions []Suggestion

	// Summary suggestion (always present)
	summary := fmt.Sprintf("Impact: %d direct callers, %d transitive, %d implementers, %d tests across %d packages",
		directCallers, transitiveCallers, implementers, tests, pkgCount)
	suggestions = append(suggestions, Suggestion{
		Description: summary,
		Priority:    1,
	})

	if tests == 0 {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe tests " + symbol,
			Description: "No test coverage found — check with transitive search",
			Priority:    1,
			Condition:   "no_tests",
		})
	}

	if transitiveCallers > 10 {
		suggestions = append(suggestions, Suggestion{
			Command:     "snipe callers --direct " + symbol,
			Description: "Many transitive callers — drill into direct callers",
			Priority:    2,
		})
	}

	return suggestions
}
```

Add `"fmt"` to the imports in types.go if not already present.

**Note:** `HintCoImplementer` is intentionally omitted — no code path produces it yet. Add when the reverse interface lookup is implemented, not before. Dead constants are noise.

**Step 2: Write the impact command**

Key design decisions reflected in this code:
- **Phase ordering preserved:** `ScoreAndSort` is intentionally skipped. For impact, the phase grouping (callers → implementers → tests) IS the meaningful ordering. Within each phase, results are already ordered by file/name from the SQL.
- **Limit budget:** Each phase uses an internal limit of `lim * 3` to allow headroom, then post-merge truncation via `TruncateToTokenBudget` enforces the actual budget. This prevents phase 1 from starving phases 2-3.
- **Degraded tracking:** Uses bool flags to avoid duplicate degraded entries.

```go
// cmd/impact.go
package cmd

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var impactCmd = &cobra.Command{
	Use:     "impact [symbol|id]",
	Short:   "Show blast radius for changing a symbol",
	GroupID: "core",
	Long: `Analyzes what breaks if a symbol changes: transitive callers,
interface implementers, and test coverage in one call.

Returns a flat result list with hint-based classification:
  direct_caller, transitive_caller, implementer, direct_test, transitive_test

Accepts symbol name, 16-char hex ID (auto-detected), or --at position.

Examples:
  snipe impact ProcessOrder            # Full blast radius
  snipe impact --direct ProcessOrder   # Direct callers + direct tests only
  snipe impact --at order.go:42:1      # By position
  snipe impact a3f2c1de89ab0123        # By hex ID`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImpact,
}

var (
	impactDirect bool
	impactAt     string
	impactID     string
)

func init() {
	impactCmd.Flags().BoolVar(&impactDirect, "direct", false, "1-hop only (skip transitive callers and tests)")
	impactCmd.Flags().StringVar(&impactAt, "at", "", "Position to look up (file:line:col)")
	impactCmd.Flags().StringVar(&impactID, "id", "", "Symbol ID to look up")
	rootCmd.AddCommand(impactCmd)
}

func runImpact(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary

	w := output.NewWriter(os.Stdout, compact)

	if len(args) == 0 && impactAt == "" && impactID == "" {
		return w.WriteError("impact", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name, --at position, or --id",
		})
	}

	s, dir, err := OpenStore(w, "impact")
	if err != nil {
		return err
	}
	defer s.Close()

	// --- Symbol resolution (same pattern as tests/callers) ---
	var symbolID string
	var queryInfo map[string]string

	switch {
	case impactID != "":
		symbolID = impactID
		queryInfo = map[string]string{"id": impactID}

	case impactAt != "":
		pos, err := query.ParsePosition(impactAt)
		if err != nil {
			return w.WriteError("impact", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		filePath := pos.File
		if filepath.IsAbs(filePath) {
			if rel, err := filepath.Rel(dir, filePath); err == nil {
				filePath = rel
			}
		}
		sym := query.FindSymbolAtPosition(s.DB(), filePath, pos.Line)
		if sym == nil {
			return w.WriteError("impact", &output.Error{
				Code:    output.ErrNotFound,
				Message: "no symbol found at " + impactAt,
			})
		}
		symbolID = sym.ID
		queryInfo = map[string]string{"at": impactAt, "resolved": sym.Name}

	default:
		name := args[0]
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				break
			}
		}
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("impact", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		if len(symbols) == 0 {
			return w.WriteError("impact", output.NewNotFoundError(name))
		}
		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, sym := range symbols {
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError("impact", output.NewAmbiguousError(name, candidates))
		}
		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
	}

	// Look up symbol metadata for hints and session tracking
	var symName, symKind string
	if sym, err := query.LookupByID(s.DB(), symbolID); err == nil && sym != nil {
		symName = sym.Name
		symKind = sym.Kind
		recordSessionQuery(dir, sym.Name, sym.FilePathRel, sym.LineStart, sym.Kind, "impact")
	}

	var degraded []string
	var bodyFailed, contextFailed bool

	// --- Phase 1: Transitive callers (non-test files) ---
	// Use internal limit 3x to avoid phase 1 starving phases 2-3
	internalLim := lim * 3
	callerRows, err := query.FindImpactCallers(s.DB(), symbolID, impactDirect, internalLim, 0)
	if err != nil {
		degraded = append(degraded, "callers_failed")
		callerRows = nil
	}

	// --- Phase 2: Interface implementers ---
	// Only meaningful for interface types — FindImplementers parses the
	// interface body to extract method names, so it needs the interface symbol.
	// Method-on-interface targets are not handled here (would need parent lookup).
	var implRows []query.SymbolRow
	if symKind == "interface" {
		implRows, err = query.FindImplementers(s.DB(), symbolID, internalLim, 0)
		if err != nil {
			degraded = append(degraded, "implementers_failed")
			implRows = nil
		}
	}

	// --- Phase 3: Test coverage ---
	testRows, err := query.FindTests(s.DB(), symbolID, impactDirect, internalLim, 0)
	if err != nil {
		degraded = append(degraded, "tests_failed")
		testRows = nil
	}

	// --- Merge phases with cross-phase dedup ---
	// A symbol can appear in multiple phases (e.g., a test helper that is
	// both a transitive_caller and a direct_test). Strategy: merge hints,
	// keep first-seen ordering (phase order = semantic priority).
	type merged struct {
		result output.Result
		hints  []string
		order  int
	}
	seen := map[string]*merged{}
	orderCounter := 0

	addOrMerge := func(id string, r output.Result, hint string) {
		if m, ok := seen[id]; ok {
			m.hints = append(m.hints, hint)
		} else {
			seen[id] = &merged{result: r, hints: []string{hint}, order: orderCounter}
			orderCounter++
		}
	}

	// Phase 1 results
	directCallerCount := 0
	transitiveCallerCount := 0
	for _, cr := range callerRows {
		hint := output.HintDirectCaller
		if cr.Hop == 2 {
			hint = output.HintTransitiveCaller
			transitiveCallerCount++
		} else {
			directCallerCount++
		}
		addOrMerge(cr.ID, cr.ToResult(), hint)
	}

	// Phase 2 results
	implementerCount := 0
	for _, ir := range implRows {
		addOrMerge(ir.ID, ir.ToResult(), output.HintImplementer)
		implementerCount++
	}

	// Phase 3 results
	testCount := 0
	for _, tr := range testRows {
		hint := output.HintDirectTest
		if tr.Hop == 2 {
			hint = output.HintTransitiveTest
		}
		addOrMerge(tr.ID, tr.ToResult(), hint)
		testCount++
	}

	// Flatten: sort by insertion order (preserves phase grouping)
	sortable := make([]struct {
		m     *merged
		order int
	}, 0, len(seen))
	for _, m := range seen {
		sortable = append(sortable, struct {
			m     *merged
			order int
		}{m: m, order: m.order})
	}
	sort.Slice(sortable, func(i, j int) bool {
		return sortable[i].order < sortable[j].order
	})

	results := make([]output.Result, 0, len(sortable))
	for _, s := range sortable {
		s.m.result.Hints = s.m.hints
		results = append(results, s.m.result)
	}

	// Batch fetch bodies if requested
	if withBody && len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		bodySymbols, batchErr := query.BatchLookupByID(s.DB(), ids)
		if batchErr != nil {
			degraded = append(degraded, "batch_lookup_failed")
		} else {
			for i, r := range results {
				if sym, ok := bodySymbols[r.ID]; ok && sym != nil {
					symResult := sym.ToResult()
					if err := output.AddBody(&symResult); err != nil && !bodyFailed {
						degraded = append(degraded, "body_extraction_failed")
						bodyFailed = true
					}
					results[i].Body = symResult.Body
				}
			}
		}
	}

	// Context lines
	if contextLines > 0 && !withBody {
		for i := range results {
			if err := output.AddContext(&results[i], contextLines); err != nil && !contextFailed {
				degraded = append(degraded, "context_extraction_failed")
				contextFailed = true
			}
		}
	}

	// NOTE: ScoreAndSort intentionally skipped for impact.
	// Phase grouping (callers → implementers → tests) IS the meaningful
	// ordering. ScoreAndSort ranks by name-similarity which destroys it.
	results = ApplySelection(results)

	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	// Apply user-requested offset/limit AFTER merge (phases use internal limits)
	if off > 0 && off < len(results) {
		results = results[off:]
	} else if off >= len(results) {
		results = nil
	}
	if len(results) > lim {
		results = results[:lim]
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	// Count packages for summary
	pkgs := map[string]bool{}
	for _, r := range results {
		if r.Package != "" {
			pkgs[r.Package] = true
		}
	}

	suggestions := output.SuggestionsForImpact(
		symName, directCallerCount, transitiveCallerCount,
		implementerCount, testCount, len(pkgs),
	)

	if summary {
		summaryData := output.BuildSummary(results)
		return w.WriteResponse(output.Response[output.Summary]{
			Protocol:    output.ProtocolVersion,
			Ok:          true,
			Results:     []output.Summary{summaryData},
			Suggestions: suggestions,
			Meta: output.Meta{
				Command:    "impact",
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

	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: suggestions,
		Meta: output.Meta{
			Command:       "impact",
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

**Step 3: Add `"impact": true` to knownSubcommands**

In `cmd/root.go`, add `"impact": true` to the core commands section:

```go
	"index": true, "def": true, "refs": true, "callers": true, "callees": true,
	"search": true, "show": true, "sym": true, "status": true, "tests": true, "impact": true,
```

**Step 4: Run `mage` to verify build + lint + tests pass**

Run: `mage`
Expected: PASS

**Step 5: Commit**

```
feat: add snipe impact command for blast radius analysis (#110)
```

---

### Task 3: Blackbox test

**Files:**
- Modify: existing blackbox test file (check `test/blackbox/` for pattern)

**Step 1: Find existing blackbox test pattern**

Run: `ls test/blackbox/`

Look at how `tests` command is tested in blackbox tests. The test should:
1. Index a Go fixture with a known call graph
2. Run `snipe impact <symbol>` and parse JSON output
3. Assert specific hint values on specific result IDs

**Step 2: Add blackbox test for impact**

Key assertions:
- Direct caller has `direct_caller` hint
- Transitive caller has `transitive_caller` hint (when `--direct` not set)
- `--direct` flag suppresses transitive results
- Tests appear with `direct_test` / `transitive_test` hints
- Cross-phase dedup: a symbol appearing as both caller and test has merged hints
- Response envelope has `"command": "impact"`, valid `suggestions` array
- Zero-result case returns `ok: true` with empty results and appropriate suggestions

**Step 3: Run `mage qa`**

Run: `mage qa`
Expected: PASS (build, lint, test, race, blackbox, govulncheck)

**Step 4: Commit**

```
test: add blackbox tests for snipe impact (#110)
```

---

### Task 4: Integration — wire up and verify end-to-end

**Step 1: Manual smoke test on snipe's own codebase**

```bash
snipe index
snipe impact SaveSymbol
snipe impact --direct SaveSymbol
snipe impact --at internal/store/write.go:1:1
snipe impact FindTests
```

Verify:
- Output has expected hint classifications
- Summary suggestion counts match visible results
- Phase ordering preserved (callers before tests)
- `--direct` suppresses transitive results in both callers and tests
- `meta.ms` is <50ms

**Step 2: Final `mage qa`**

Run: `mage qa`
Expected: PASS

**Step 3: Commit any fixups**

Only if smoke test revealed issues.

---

### Design Decisions Recorded Here

| Decision | Rationale |
|----------|-----------|
| Skip `ScoreAndSort` | Phase grouping IS the meaningful ordering for impact; name-similarity scoring destroys it |
| Internal limit 3× user limit | Prevents phase 1 from starving phases 2-3; post-merge truncation enforces actual budget |
| `sort.Slice` not bubble sort | Standard library, O(n log n), readable |
| Phase 2 only for `symKind == "interface"` | `FindImplementers` parses interface body for methods. For methods-on-interface, would need parent lookup — deferred |
| No `HintCoImplementer` constant | No code path produces it yet. Add when reverse interface lookup exists, not before |
| `execOrFatal` test helper | Prevents silent test setup failures from wrong column counts |
| Bool flags for degraded dedup | Avoids appending duplicate degraded strings per-result |
| Suggestions in summary mode too | Summary path now includes suggestions (original plan returned early before computing them) |
