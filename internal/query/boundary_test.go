package query

import (
	"database/sql"
	"reflect"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
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

// seedBoundaryFixture builds a tiny in-memory schema with the columns the
// real index has (subset). Avoids spinning up a real index for unit tests.
func seedBoundaryFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
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
