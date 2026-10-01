package query_test

import (
	"database/sql"
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

// seedDeadcodeDB: Used (referenced from prod), TestOnly (referenced only from a
// test), Spawned (only a synthetic go self-ref), Dead, unexported, main, and a
// test-file export. Two packages so the pkg filter has something to drop.
func seedDeadcodeDB(t *testing.T) *sql.DB {
	t.Helper()
	s := openLitTestStore(t)
	_, err := s.DB().Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end) VALUES
			('u', 'Used',     'func', '/r/a/a.go',      'a/a.go',      'ex.com/m/a', 1, 1, 2, 1),
			('t', 'TestOnly', 'func', '/r/a/a.go',      'a/a.go',      'ex.com/m/a', 3, 1, 4, 1),
			('g', 'Spawned',  'func', '/r/a/a.go',      'a/a.go',      'ex.com/m/a', 5, 1, 6, 1),
			('d', 'Dead',     'type', '/r/a/a.go',      'a/a.go',      'ex.com/m/a', 7, 1, 8, 1),
			('p', 'private',  'func', '/r/a/a.go',      'a/a.go',      'ex.com/m/a', 9, 1, 10, 1),
			('mn','main',     'func', '/r/main.go',     'main.go',     'ex.com/m',   1, 1, 2, 1),
			('x', 'Helper',   'func', '/r/a/a_test.go', 'a/a_test.go', 'ex.com/m/a', 1, 1, 2, 1),
			('o', 'Other',    'var',  '/r/b/b.go',      'b/b.go',      'ex.com/m/b', 1, 1, 1, 9);
		INSERT INTO refs (id, symbol_id, file_path, line, col, enclosing_id, ast_ctx) VALUES
			('r1', 'u', '/r/b/b.go',      2, 1, 'o', NULL),
			('r2', 't', '/r/a/a_test.go', 2, 1, 'x', NULL),
			('r3', 'g', '/r/a/a.go',      5, 1, 'g', 'go');`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s.DB()
}

func deadNames(rows []query.DeadExport) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

func TestFindDeadExports(t *testing.T) {
	tests := []struct {
		name         string
		pkg          string
		includeTests bool
		want         []string
	}{
		{name: "test refs ignored", want: []string{"Dead", "Spawned", "TestOnly", "Other"}},
		{name: "test refs counted", includeTests: true, want: []string{"Dead", "Spawned", "Other"}},
		{name: "pkg suffix filter", pkg: "b", want: []string{"Other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := seedDeadcodeDB(t)
			got, err := query.FindDeadExports(db, tt.pkg, tt.includeTests)
			if err != nil {
				t.Fatalf("FindDeadExports: %v", err)
			}
			names := deadNames(got)
			if len(names) != len(tt.want) {
				t.Fatalf("FindDeadExports = %v, want %v", names, tt.want)
			}
			for i := range names {
				if names[i] != tt.want[i] {
					t.Fatalf("FindDeadExports = %v, want %v", names, tt.want)
				}
			}
		})
	}
}

func TestFindDeadExports_ReportsAllFileRefCount_When_TestRefsIgnored(t *testing.T) {
	db := seedDeadcodeDB(t)
	got, err := query.FindDeadExports(db, "", false)
	if err != nil {
		t.Fatalf("FindDeadExports: %v", err)
	}
	for _, r := range got {
		if r.Name == "TestOnly" {
			if r.RefsAll != 1 || r.File != "a/a.go" || r.Line != 3 || r.Pkg != "ex.com/m/a" {
				t.Errorf("TestOnly row = %+v, want RefsAll 1 at a/a.go:3 in ex.com/m/a", r)
			}
			return
		}
	}
	t.Fatal("TestOnly not reported")
}

func TestCountCallersAndCallees(t *testing.T) {
	s := openLitTestStore(t)
	_, err := s.DB().Exec(`
		INSERT INTO symbols (id, name, kind, file_path, line_start, col_start, line_end, col_end) VALUES
			('a', 'A', 'func', '/r/x.go', 1, 1, 2, 1),
			('b', 'B', 'func', '/r/x.go', 3, 1, 4, 1),
			('c', 'C', 'func', '/r/x.go', 5, 1, 6, 1);
		INSERT INTO call_graph (caller_id, callee_id, file_path, line, col) VALUES
			('a', 'c', '/r/x.go', 1, 5),
			('a', 'c', '/r/x.go', 1, 9),
			('b', 'c', '/r/x.go', 3, 5);`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	tests := []struct {
		id               string
		callers, callees int
	}{
		{id: "a", callers: 0, callees: 2},
		{id: "c", callers: 3, callees: 0},
		{id: "missing", callers: 0, callees: 0},
	}
	for _, tt := range tests {
		callers, err := query.CountCallers(s.DB(), tt.id)
		if err != nil || callers != tt.callers {
			t.Errorf("CountCallers(%q) = %d, %v; want %d", tt.id, callers, err, tt.callers)
		}
		callees, err := query.CountCallees(s.DB(), tt.id)
		if err != nil || callees != tt.callees {
			t.Errorf("CountCallees(%q) = %d, %v; want %d", tt.id, callees, err, tt.callees)
		}
	}
}
