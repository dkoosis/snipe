package query

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestExtractInterfaceMethodNames(t *testing.T) {
	content := `package fake

type Greeter interface {
	Hello() string
	Goodbye(name string) error
	// comment
	unexportedHelper()
}
`
	f := writeTempGoFile(t, content)
	methods := ExtractInterfaceMethodNames(f, 3, 8)

	if len(methods) != 2 {
		t.Fatalf("expected 2 methods, got %d: %v", len(methods), methods)
	}
	if methods[0] != "Hello" {
		t.Errorf("expected Hello, got %s", methods[0])
	}
	if methods[1] != "Goodbye" {
		t.Errorf("expected Goodbye, got %s", methods[1])
	}
}

func TestExtractInterfaceMethodNames_SkipsEmbedded(t *testing.T) {
	content := `package fake

type ReadWriter interface {
	io.Reader
	Write(p []byte) (n int, err error)
}
`
	f := writeTempGoFile(t, content)
	methods := ExtractInterfaceMethodNames(f, 3, 6)
	if len(methods) != 1 || methods[0] != "Write" {
		t.Errorf("expected [Write], got %v", methods)
	}
}

func TestLookupByName_MethodByBareName(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
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
		CREATE TABLE files (path TEXT PRIMARY KEY, hash TEXT);
		CREATE INDEX idx_symbols_name ON symbols(name);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// Insert a method with a receiver and a plain function
	_, err = db.Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('aaa', 'ListFiles', 'method', '/src/disk.go', 'disk.go', 'pkg/disk', 10, 1, 20, 1, '(*DiskSpacePrimitives)');
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('bbb', 'ListFiles', 'method', '/src/mem.go', 'mem.go', 'pkg/mem', 30, 1, 40, 1, '(*MemoryStore)');
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end)
		VALUES ('ccc', 'OtherFunc', 'func', '/src/other.go', 'other.go', 'pkg/other', 1, 1, 5, 1);
	`)
	if err != nil {
		t.Fatalf("Failed to insert symbols: %v", err)
	}

	// Bare name "ListFiles" should find both methods even without receiver syntax
	results, err := LookupByName(db, "ListFiles")
	if err != nil {
		t.Fatalf("LookupByName error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for bare method name, got %d", len(results))
	}
	for _, r := range results {
		if r.Name != "ListFiles" {
			t.Errorf("unexpected name: %s", r.Name)
		}
		if !r.Receiver.Valid || r.Receiver.String == "" {
			t.Error("expected receiver to be set")
		}
	}
}

// Regression: T.Method form must resolve when T is unexported (e.g.
// `node.put`, `freelist.Free` against bbolt-style internal types). The earlier
// heuristic only attempted method lookup when the receiver name began with an
// uppercase letter, falling through to package-qualified lookup otherwise.
func TestLookupByName_UnexportedTypeMethod(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
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
		CREATE TABLE files (path TEXT PRIMARY KEY, hash TEXT);
		CREATE INDEX idx_symbols_name ON symbols(name);
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('1', 'put', 'method', '/repo/node.go', 'node.go', 'go.etcd.io/bbolt', 117, 1, 142, 1, '(*node)');
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	results, err := LookupByName(db, "node.put")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for node.put, got %d", len(results))
	}
	if results[0].Name != "put" {
		t.Errorf("unexpected name: %s", results[0].Name)
	}
	if !results[0].Receiver.Valid || results[0].Receiver.String != "(*node)" {
		t.Errorf("unexpected receiver: %v", results[0].Receiver)
	}
}

func TestLookupByName_ExactMatchTakesPrecedence(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
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
		CREATE TABLE files (path TEXT PRIMARY KEY, hash TEXT);
		CREATE INDEX idx_symbols_name ON symbols(name);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// Insert both a function and a method with the same name
	_, err = db.Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end)
		VALUES ('aaa', 'Close', 'func', '/src/util.go', 'util.go', 'pkg/util', 1, 1, 5, 1);
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('bbb', 'Close', 'method', '/src/conn.go', 'conn.go', 'pkg/conn', 10, 1, 20, 1, '(*Conn)');
	`)
	if err != nil {
		t.Fatalf("Failed to insert symbols: %v", err)
	}

	// Exact match should return both (function + method) from the first query tier
	results, err := LookupByName(db, "Close")
	if err != nil {
		t.Fatalf("LookupByName error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for exact match, got %d", len(results))
	}
}

func writeTempGoFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

// The disambiguation round-trip must need zero hex IDs: forms snipe itself
// prints (and the pkg-qualified forms a caller would naturally type) all
// resolve back to the right symbol.
func TestLookupByName_QualifiedMethodForms(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT,
			file_path TEXT, file_path_rel TEXT, pkg_path TEXT,
			line_start INTEGER, col_start INTEGER, line_end INTEGER, col_end INTEGER,
			signature TEXT, doc TEXT, receiver TEXT
		);
		CREATE TABLE files (path TEXT PRIMARY KEY, hash TEXT);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}
	_, err = db.Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('aaa', 'Get', 'method', '/src/store/s.go', 'store/s.go', 'github.com/x/proj/internal/store', 10, 1, 20, 1, '(*Store)');
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end, receiver)
		VALUES ('bbb', 'Get', 'method', '/src/cache/c.go', 'cache/c.go', 'github.com/x/proj/internal/cache', 30, 1, 40, 1, '(*Cache)');
	`)
	if err != nil {
		t.Fatalf("Failed to insert symbols: %v", err)
	}

	cases := []struct {
		query  string
		wantID string
	}{
		{"Store.Get", "aaa"},          // T.Method
		{"(*Store).Get", "aaa"},       // displayed form
		{"((*Store)).Get", "aaa"},     // legacy double-paren display, pasted back
		{"store.Store.Get", "aaa"},    // pkg.T.Method
		{"cache.Cache.Get", "bbb"},    // pkg.T.Method, other package
		{"store.(*Store).Get", "aaa"}, // pkg.(*T).Method
	}
	for _, tc := range cases {
		results, err := LookupByName(db, tc.query)
		if err != nil {
			t.Fatalf("LookupByName(%q) error: %v", tc.query, err)
		}
		if len(results) != 1 {
			t.Errorf("LookupByName(%q) = %d results, want 1", tc.query, len(results))
			continue
		}
		if results[0].ID != tc.wantID {
			t.Errorf("LookupByName(%q) = id %s, want %s", tc.query, results[0].ID, tc.wantID)
		}
	}
}
