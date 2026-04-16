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

func insertTestSym(t *testing.T, db *sql.DB, id, name, filePath, filePathRel, pkgPath, signature string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
		line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, 'func', ?, ?, ?, 1, 1, 10, 1, ?, '', '')`,
		id, name, filePath, filePathRel, pkgPath, signature)
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

	insertTestSym(t, db, "s1", "ProcessOrder", "/repo/order.go", "order.go", "pkg/order", "func ProcessOrder()")
	insertTestSym(t, db, "t1", "TestProcessOrder", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestProcessOrder(t *testing.T)")
	insertCallEdge(t, db, "t1", "s1", "/repo/order_test.go", 10, 5)
	insertTestSym(t, db, "c1", "HandleRequest", "/repo/handler.go", "handler.go", "pkg/handler", "func HandleRequest()")
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

	insertTestSym(t, db, "s1", "ProcessOrder", "/repo/order.go", "order.go", "pkg/order", "func ProcessOrder()")
	insertTestSym(t, db, "h1", "setupOrder", "/repo/order_test.go", "order_test.go", "pkg/order", "func setupOrder()")
	insertCallEdge(t, db, "h1", "s1", "/repo/order_test.go", 15, 5)
	insertTestSym(t, db, "t1", "TestOrderFlow", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestOrderFlow(t *testing.T)")
	insertCallEdge(t, db, "t1", "h1", "/repo/order_test.go", 25, 5)
	insertTestSym(t, db, "t2", "TestProcessOrder", "/repo/order_test.go", "order_test.go", "pkg/order", "func TestProcessOrder(t *testing.T)")
	insertCallEdge(t, db, "t2", "s1", "/repo/order_test.go", 30, 5)

	results, err := FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 tests, got %d", len(results))
	}
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

	insertTestSym(t, db, "s1", "Foo", "/repo/foo.go", "foo.go", "pkg/foo", "func Foo()")
	for _, tc := range []struct{ id, name string }{
		{"t1", "TestFoo"},
		{"t2", "BenchmarkFoo"},
		{"t3", "FuzzFoo"},
		{"t4", "ExampleFoo"},
	} {
		insertTestSym(t, db, tc.id, tc.name, "/repo/foo_test.go", "foo_test.go", "pkg/foo", "")
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

	insertTestSym(t, db, "s1", "Untested", "/repo/lonely.go", "lonely.go", "pkg/lonely", "func Untested()")

	results, err := FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 tests, got %d", len(results))
	}
}

func TestFindTests_DeduplicatesMultipleCallEdges(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	// Test calls the target on three different lines.
	// Should produce ONE result, not three.
	insertTestSym(t, db, "s1", "Target", "/repo/target.go", "target.go", "pkg/t", "func Target()")
	insertTestSym(t, db, "t1", "TestTarget", "/repo/target_test.go", "target_test.go", "pkg/t", "func TestTarget(t *testing.T)")
	insertCallEdge(t, db, "t1", "s1", "/repo/target_test.go", 10, 5)
	insertCallEdge(t, db, "t1", "s1", "/repo/target_test.go", 20, 5)
	insertCallEdge(t, db, "t1", "s1", "/repo/target_test.go", 30, 5)

	// direct=true path
	results, err := FindTests(db, "s1", true, 50, 0)
	if err != nil {
		t.Fatalf("FindTests direct: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("direct: expected 1 (deduped), got %d", len(results))
	}

	// direct=false path (transitive)
	results, err = FindTests(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatalf("FindTests transitive: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("transitive: expected 1 (deduped), got %d", len(results))
	}
}

func TestFindTests_DeduplicatesDirectAndTransitive(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	insertTestSym(t, db, "s1", "Target", "/repo/target.go", "target.go", "pkg/t", "func Target()")
	insertTestSym(t, db, "h1", "helper", "/repo/target_test.go", "target_test.go", "pkg/t", "func helper()")
	insertCallEdge(t, db, "h1", "s1", "/repo/target_test.go", 5, 1)
	insertTestSym(t, db, "t1", "TestBoth", "/repo/target_test.go", "target_test.go", "pkg/t", "func TestBoth(t *testing.T)")
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
