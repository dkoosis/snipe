package query

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupImpactTestDB(t *testing.T) *sql.DB {
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

func execOrFatal(t *testing.T, db *sql.DB, query string, args ...interface{}) { //nolint:unparam // variadic kept for future use
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestFindImpactCallers_Direct(t *testing.T) {
	db := setupImpactTestDB(t)

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
	if rows[0].ID != "bbb" { //nolint:goconst // test fixture IDs are intentionally literal
		t.Errorf("expected caller bbb, got %s", rows[0].ID)
	}
	if rows[0].Hop != 1 {
		t.Errorf("expected hop 1, got %d", rows[0].Hop)
	}
}

func TestFindImpactCallers_Transitive(t *testing.T) {
	db := setupImpactTestDB(t)

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
	if rows[0].Hop != 1 || rows[0].ID != "bbb" {
		t.Errorf("first result should be direct caller bbb hop=1, got %s hop=%d", rows[0].ID, rows[0].Hop)
	}
	if rows[1].Hop != 2 || rows[1].ID != "ccc" {
		t.Errorf("second result should be transitive caller ccc hop=2, got %s hop=%d", rows[1].ID, rows[1].Hop)
	}
}

func TestFindImpactCallers_DirectOnly(t *testing.T) {
	db := setupImpactTestDB(t)

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

	execOrFatal(t, db, `INSERT INTO symbols VALUES ('aaa','funcA','func','/f.go','f.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('bbb','funcB','func','/g.go','g.go','pkg',10,1,15,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('ddd','funcD','func','/d.go','d.go','pkg',30,1,35,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/f.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/g.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/d.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','aaa','/g.go',12,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('ddd','aaa','/d.go',32,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('bbb','ddd','/g.go',13,5)`)

	rows, err := FindImpactCallers(db, "aaa", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}

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

func TestFindMethodIDs(t *testing.T) {
	db := setupImpactTestDB(t)

	execOrFatal(t, db, `INSERT INTO symbols VALUES ('s1','Store','struct','/store.go','store.go','pkg/store',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m1','Open','method','/store.go','store.go','pkg/store',10,1,20,1,NULL,NULL,'(*Store)')`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m2','Close','method','/store.go','store.go','pkg/store',25,1,30,1,NULL,NULL,'(*Store)')`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m3','Get','method','/other.go','other.go','pkg/other',1,1,5,1,NULL,NULL,'(*Other)')`)

	ids, err := FindMethodIDs(db, "Store")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(ids))
	}
}

func TestFindImpactCallersMulti(t *testing.T) {
	db := setupImpactTestDB(t)

	// Store struct + two methods
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('s1','Store','struct','/store.go','store.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m1','Open','method','/store.go','store.go','pkg',10,1,20,1,NULL,NULL,'(*Store)')`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m2','Close','method','/store.go','store.go','pkg',25,1,30,1,NULL,NULL,'(*Store)')`)
	// Callers of the methods
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('c1','funcA','func','/a.go','a.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('c2','funcB','func','/b.go','b.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/store.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/a.go',0,'h2')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/b.go',0,'h3')`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('c1','m1','/a.go',3,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('c2','m2','/b.go',3,5)`)

	// Single ID returns nothing (struct has no call edges)
	rows, err := FindImpactCallers(db, "s1", false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("single struct ID should have 0 callers, got %d", len(rows))
	}

	// Multi with methods returns callers
	rows, err = FindImpactCallersMulti(db, []string{"s1", "m1", "m2"}, false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 callers via methods, got %d", len(rows))
	}
}

func TestFindImpactCallersMulti_Dedup(t *testing.T) {
	db := setupImpactTestDB(t)

	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m1','Open','method','/store.go','store.go','pkg',10,1,20,1,NULL,NULL,'(*Store)')`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('m2','Close','method','/store.go','store.go','pkg',25,1,30,1,NULL,NULL,'(*Store)')`)
	execOrFatal(t, db, `INSERT INTO symbols VALUES ('c1','funcA','func','/a.go','a.go','pkg',1,1,5,1,NULL,NULL,NULL)`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/store.go',0,'h1')`)
	execOrFatal(t, db, `INSERT INTO files VALUES ('/a.go',0,'h2')`)
	// c1 calls both methods
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('c1','m1','/a.go',3,5)`)
	execOrFatal(t, db, `INSERT INTO call_graph VALUES ('c1','m2','/a.go',4,5)`)

	rows, err := FindImpactCallersMulti(db, []string{"m1", "m2"}, false, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 deduped caller, got %d", len(rows))
	}
}

func TestFindImpactCallers_ExcludesTestFiles(t *testing.T) {
	db := setupImpactTestDB(t)

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
