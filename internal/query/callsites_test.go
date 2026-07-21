package query

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// setupCallSitesDB builds an in-memory schema that includes refs.ast_ctx (the
// column FindCallSites/CountCallSites read) plus the enclosing-symbol join.
func setupCallSitesDB(t *testing.T) *sql.DB {
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
		CREATE TABLE refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			file_path_rel TEXT,
			line INTEGER,
			col INTEGER,
			enclosing_id TEXT,
			snippet TEXT,
			ast_ctx TEXT
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

func insertCSSym(t *testing.T, db *sql.DB, id, name, filePathRel, pkgPath string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
		line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, 'func', ?, ?, ?, 1, 1, 10, 1, '', '', '')`,
		id, name, "/repo/"+filePathRel, filePathRel, pkgPath)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", id, err)
	}
}

// insertCSRef inserts a ref. Pass astCtx="" to store SQL NULL (simulates a
// pre-v18 / not-backfilled index); any other value is stored literally.
func insertCSRef(t *testing.T, db *sql.DB, id, filePathRel string, line, col int, enclosingID, snippet, astCtx string) {
	t.Helper()
	var enc any
	if enclosingID == "" {
		enc = nil
	} else {
		enc = enclosingID
	}
	var ctx any
	if astCtx == "" {
		ctx = nil
	} else {
		ctx = astCtx
	}
	_, err := db.Exec(`INSERT INTO refs (id, symbol_id, file_path, file_path_rel, line, col, enclosing_id, snippet, ast_ctx)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "tgt", "/repo/"+filePathRel, filePathRel, line, col, enc, snippet, ctx)
	if err != nil {
		t.Fatalf("insert ref %s: %v", id, err)
	}
}

func TestFindCallSites_GroupOrderAndFields(t *testing.T) {
	db := setupCallSitesDB(t)
	defer db.Close()

	insertCSSym(t, db, "tgt", "Target", "foo/foo.go", "example.com/p/foo")
	insertCSSym(t, db, "serve", "Serve", "api/api.go", "example.com/p/api")
	insertCSSym(t, db, "run", "Run", "worker/worker.go", "example.com/p/worker")

	// worker ref inserted first, but ORDER BY file_path_rel must put api before worker.
	insertCSRef(t, db, "r1", "worker/worker.go", 5, 9, "run", "foo.Target(id)", "")
	insertCSRef(t, db, "r2", "api/api.go", 8, 9, "serve", "foo.Target(id)", "")
	insertCSRef(t, db, "r3", "api/api.go", 12, 9, "serve", "wire(foo.Target)", "call:wire")

	rows, err := FindCallSites(db, "tgt")
	if err != nil {
		t.Fatalf("FindCallSites: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 call sites, got %d", len(rows))
	}
	// api rows (r2, r3) must come before the worker row (r1) — sorted by file_path_rel.
	if rows[0].FilePathRel != "api/api.go" || rows[1].FilePathRel != "api/api.go" || rows[2].FilePathRel != "worker/worker.go" {
		t.Fatalf("unexpected order: %s, %s, %s", rows[0].FilePathRel, rows[1].FilePathRel, rows[2].FilePathRel)
	}
	if rows[0].EnclosingName != "Serve" {
		t.Fatalf("enclosing name not populated: name=%q", rows[0].EnclosingName)
	}
	if rows[1].ASTCtx != "call:wire" {
		t.Fatalf("want ast_ctx call:wire on r3, got %q", rows[1].ASTCtx)
	}
	for i := range rows {
		if rows[i].IsTest {
			t.Fatalf("row %d unexpectedly flagged IsTest", i)
		}
	}
}

func TestFindCallSites_NullASTCtxDegrades(t *testing.T) {
	db := setupCallSitesDB(t)
	defer db.Close()

	insertCSSym(t, db, "tgt", "Target", "foo/foo.go", "example.com/p/foo")
	insertCSSym(t, db, "serve", "Serve", "api/api.go", "example.com/p/api")
	insertCSRef(t, db, "r1", "api/api.go", 8, 9, "serve", "foo.Target(id)", "") // NULL ast_ctx

	rows, err := FindCallSites(db, "tgt")
	if err != nil {
		t.Fatalf("FindCallSites: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 call site, got %d", len(rows))
	}
	if rows[0].ASTCtx != "" {
		t.Fatalf("NULL ast_ctx should scan as empty string, got %q", rows[0].ASTCtx)
	}
}

func TestFindCallSites_PackageLevelEnclosingNull(t *testing.T) {
	db := setupCallSitesDB(t)
	defer db.Close()

	insertCSSym(t, db, "tgt", "Target", "foo/foo.go", "example.com/p/foo")
	insertCSRef(t, db, "r1", "api/api.go", 3, 9, "", "var _ = foo.Target(\"init\")", "") // package-level init

	rows, err := FindCallSites(db, "tgt")
	if err != nil {
		t.Fatalf("FindCallSites: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 call site, got %d", len(rows))
	}
	if rows[0].EnclosingID.Valid {
		t.Fatalf("expected NULL enclosing_id, got %q", rows[0].EnclosingID.String)
	}
	if rows[0].ID != "r1" {
		t.Fatalf("ref id should stay addressable, got %q", rows[0].ID)
	}
}

func TestFindCallSites_FiltersGoChan(t *testing.T) {
	db := setupCallSitesDB(t)
	defer db.Close()

	insertCSSym(t, db, "tgt", "Target", "foo/foo.go", "example.com/p/foo")
	insertCSRef(t, db, "r1", "api/api.go", 8, 9, "", "foo.Target(id)", "")
	insertCSRef(t, db, "r2", "api/api.go", 9, 9, "", "go foo.Target()", "go")
	insertCSRef(t, db, "r3", "api/api.go", 10, 9, "", "ch <- foo.Target()", "chan")

	rows, err := FindCallSites(db, "tgt")
	if err != nil {
		t.Fatalf("FindCallSites: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("go/chan self-refs should be filtered; want 1, got %d", len(rows))
	}
	if rows[0].ID != "r1" {
		t.Fatalf("wrong row survived: %q", rows[0].ID)
	}
}

func TestCountCallSites_NonTestVsTestSplit(t *testing.T) {
	db := setupCallSitesDB(t)
	defer db.Close()

	insertCSSym(t, db, "tgt", "Orphan", "foo/foo.go", "example.com/p/foo")
	// Two test-file refs, zero production refs — the delete fast path shape.
	insertCSRef(t, db, "r1", "foo/foo_test.go", 9, 3, "t1", "Orphan()", "")
	insertCSRef(t, db, "r2", "foo/foo_test.go", 31, 3, "t2", "Orphan()", "")
	// A go-self-ref must not be counted.
	insertCSRef(t, db, "r3", "foo/foo.go", 40, 3, "", "go Orphan()", "go")

	nonTest, testOnly, err := CountCallSites(db, "tgt")
	if err != nil {
		t.Fatalf("CountCallSites: %v", err)
	}
	if nonTest != 0 {
		t.Fatalf("want 0 non-test refs, got %d", nonTest)
	}
	if testOnly != 2 {
		t.Fatalf("want 2 test-only refs, got %d", testOnly)
	}
}
