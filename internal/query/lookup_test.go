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
