package lifecycle

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupCallersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

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

func insertSym(t *testing.T, db *sql.DB, id, name, file string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO symbols VALUES (?,?,?,?,?,'pkg',1,1,5,1,NULL,NULL,NULL)`,
		id, name, "func", "/"+file, file,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO files VALUES (?,0,'hash')`, "/"+file,
	); err != nil {
		t.Fatal(err)
	}
}

func insertCall(t *testing.T, db *sql.DB, callerID, calleeID string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO call_graph VALUES (?,?,'/x.go',1,1)`, callerID, calleeID,
	); err != nil {
		t.Fatal(err)
	}
}

func TestWalkCallers_Depth1(t *testing.T) {
	db := setupCallersTestDB(t)
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "caller1", "caller1", "caller1.go")
	insertCall(t, db, "caller1", "fn")

	nodes := WalkCallers(db, "fn", 1)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 caller at depth 1, got %d", len(nodes))
	}
	if nodes[0].ID != "caller1" {
		t.Errorf("expected caller1, got %s", nodes[0].ID)
	}
	if nodes[0].Depth != 1 {
		t.Errorf("expected depth 1, got %d", nodes[0].Depth)
	}
}

func TestWalkCallers_Depth2(t *testing.T) {
	db := setupCallersTestDB(t)
	// chain: fn <- b <- c
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "b", "b", "b.go")
	insertSym(t, db, "c", "c", "c.go")
	insertCall(t, db, "b", "fn")
	insertCall(t, db, "c", "b")

	nodes := WalkCallers(db, "fn", 2)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 callers, got %d: %v", len(nodes), nodes)
	}
	byID := map[string]int{}
	for _, n := range nodes {
		byID[n.ID] = n.Depth
	}
	if byID["b"] != 1 {
		t.Errorf("b should be depth 1, got %d", byID["b"])
	}
	if byID["c"] != 2 {
		t.Errorf("c should be depth 2, got %d", byID["c"])
	}
}

func TestWalkCallers_Depth3(t *testing.T) {
	db := setupCallersTestDB(t)
	// chain: fn <- b <- c <- d
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "b", "b", "b.go")
	insertSym(t, db, "c", "c", "c.go")
	insertSym(t, db, "d", "d", "d.go")
	insertCall(t, db, "b", "fn")
	insertCall(t, db, "c", "b")
	insertCall(t, db, "d", "c")

	nodes := WalkCallers(db, "fn", 3)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 callers at depth 3, got %d", len(nodes))
	}
	byID := map[string]int{}
	for _, n := range nodes {
		byID[n.ID] = n.Depth
	}
	if byID["b"] != 1 || byID["c"] != 2 || byID["d"] != 3 {
		t.Errorf("wrong depths: %v", byID)
	}
}

func TestWalkCallers_DepthBound(t *testing.T) {
	db := setupCallersTestDB(t)
	// chain depth 4: fn <- b <- c <- d <- e; depth=3 stops before e
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "b", "b", "b.go")
	insertSym(t, db, "c", "c", "c.go")
	insertSym(t, db, "d", "d", "d.go")
	insertSym(t, db, "e", "e", "e.go")
	insertCall(t, db, "b", "fn")
	insertCall(t, db, "c", "b")
	insertCall(t, db, "d", "c")
	insertCall(t, db, "e", "d")

	nodes := WalkCallers(db, "fn", 3)
	if len(nodes) != 3 {
		t.Fatalf("expected 3 callers (depth=3 stops at d), got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.ID == "e" {
			t.Error("e at depth 4 should be excluded")
		}
	}
}

func TestWalkCallers_CycleDetection(t *testing.T) {
	db := setupCallersTestDB(t)
	// mutual recursion: fn <- b <- fn (cycle)
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "b", "b", "b.go")
	insertCall(t, db, "b", "fn")
	insertCall(t, db, "fn", "b") // fn also calls b — creates a cycle

	// Should not hang; should return b exactly once.
	nodes := WalkCallers(db, "fn", 5)
	count := 0
	for _, n := range nodes {
		if n.ID == "b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("b should appear exactly once, got %d", count)
	}
}

func TestWalkCallers_ZeroDepth(t *testing.T) {
	db := setupCallersTestDB(t)
	insertSym(t, db, "fn", "fn", "fn.go")
	insertSym(t, db, "b", "b", "b.go")
	insertCall(t, db, "b", "fn")

	nodes := WalkCallers(db, "fn", 0)
	if nodes != nil {
		t.Errorf("depth=0 should return nil, got %v", nodes)
	}
}

func TestWalkCallers_NoCallers(t *testing.T) {
	db := setupCallersTestDB(t)
	insertSym(t, db, "fn", "fn", "fn.go")

	nodes := WalkCallers(db, "fn", 3)
	if len(nodes) != 0 {
		t.Errorf("expected 0 callers, got %d", len(nodes))
	}
}
