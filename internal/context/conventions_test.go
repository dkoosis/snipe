package context

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupConventionsDB(t *testing.T) *sql.DB {
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
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func insertSym(t *testing.T, db *sql.DB, id, name, kind, filePath, pkgPath, signature, receiver string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
		line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, ?, ?, '', ?, 1, 1, 10, 1, ?, '', ?)`,
		id, name, kind, filePath, pkgPath, signature, receiver)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", id, err)
	}
}

func TestDetectConstructors(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// 3 constructors with error, 1 without
	insertSym(t, db, "c1", "NewStore", "func", "/repo/store.go", "pkg/store", "func NewStore(path string) (*Store, error)", "")
	insertSym(t, db, "c2", "NewIndex", "func", "/repo/index.go", "pkg/index", "func NewIndex(db *sql.DB) (*Index, error)", "")
	insertSym(t, db, "c3", "NewConfig", "func", "/repo/config.go", "pkg/config", "func NewConfig() (*Config, error)", "")
	insertSym(t, db, "c4", "NewBuffer", "func", "/repo/buf.go", "pkg/buf", "func NewBuffer(size int) *Buffer", "")
	// Non-constructor should be ignored
	insertSym(t, db, "c5", "Open", "func", "/repo/store.go", "pkg/store", "func Open() error", "")

	result := detectConstructors(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Total != 4 {
		t.Errorf("total: got %d, want 4", result.Total)
	}
	if result.WithError != 3 {
		t.Errorf("with_error: got %d, want 3", result.WithError)
	}
	if result.WithoutErr != 1 {
		t.Errorf("without_error: got %d, want 1", result.WithoutErr)
	}
	if result.Pattern != "New* constructors return (T, error)" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
	if result.Confidence != "medium" {
		t.Errorf("confidence: got %q, want medium", result.Confidence)
	}
}

func TestDetectConstructors_Empty(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	result := detectConstructors(db)
	if result != nil {
		t.Error("expected nil for empty db")
	}
}

func TestDetectReceivers(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// Single-letter receivers (pointer)
	insertSym(t, db, "r1", "Open", "method", "/repo/store.go", "pkg/store", "func (s *Store) Open()", "(*s)")
	insertSym(t, db, "r2", "Close", "method", "/repo/store.go", "pkg/store", "func (s *Store) Close()", "(*s)")
	insertSym(t, db, "r3", "Get", "method", "/repo/index.go", "pkg/index", "func (i *Index) Get()", "(*i)")
	// Descriptive receiver (value)
	insertSym(t, db, "r4", "String", "method", "/repo/config.go", "pkg/config", "func (cfg Config) String()", "(cfg)")

	result := detectReceivers(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Total != 4 {
		t.Errorf("total: got %d, want 4", result.Total)
	}
	if result.SingleLetter != 3 {
		t.Errorf("single_letter: got %d, want 3", result.SingleLetter)
	}
	if result.Descriptive != 1 {
		t.Errorf("descriptive: got %d, want 1", result.Descriptive)
	}
	if result.Pattern != "single-letter receivers" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
	// 3 out of 4 are pointer receivers
	if result.PointerPct != 75.0 {
		t.Errorf("pointer_pct: got %f, want 75.0", result.PointerPct)
	}
}

func TestDetectTesting(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// Colocated test files: test file + source file in same dir
	insertSym(t, db, "t1", "TestOpen", "func", "/repo/store_test.go", "pkg/store", "", "")
	insertSym(t, db, "t2", "TestClose", "func", "/repo/store_test.go", "pkg/store", "", "")
	insertSym(t, db, "s1", "Open", "func", "/repo/store.go", "pkg/store", "", "")

	// Separate test file (no source file in same dir)
	insertSym(t, db, "t3", "TestIntegration", "func", "/repo/test/integration_test.go", "pkg/test", "", "")

	// Test helper (unexported func in test file)
	insertSym(t, db, "t4", "setupDB", "func", "/repo/store_test.go", "pkg/store", "", "")

	result := detectTesting(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TestFiles != 2 {
		t.Errorf("test_files: got %d, want 2", result.TestFiles)
	}
	if result.Colocated != 1 {
		t.Errorf("colocated: got %d, want 1", result.Colocated)
	}
	if result.Separate != 1 {
		t.Errorf("separate: got %d, want 1", result.Separate)
	}
	if result.Helpers != 1 {
		t.Errorf("helpers: got %d, want 1", result.Helpers)
	}
	if result.Pattern != "colocated test files" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
}

func TestDetectInterfaces(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// -er suffix interfaces
	insertSym(t, db, "i1", "Reader", "interface", "/repo/io.go", "pkg/io", "", "")
	insertSym(t, db, "i2", "Writer", "interface", "/repo/io.go", "pkg/io", "", "")
	insertSym(t, db, "i3", "Executor", "interface", "/repo/exec.go", "pkg/exec", "", "")
	// Noun-based
	insertSym(t, db, "i4", "Store", "interface", "/repo/store.go", "pkg/store", "", "")
	// Unexported — should be excluded by GLOB '[A-Z]*'
	insertSym(t, db, "i5", "helper", "interface", "/repo/util.go", "pkg/util", "", "")

	result := detectInterfaces(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Total != 4 {
		t.Errorf("total: got %d, want 4", result.Total)
	}
	if result.ErSuffix != 3 {
		t.Errorf("er_suffix: got %d, want 3", result.ErSuffix)
	}
	if result.Pattern != "-er/-or suffix naming" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
	if result.Confidence != "medium" {
		t.Errorf("confidence: got %q, want medium", result.Confidence)
	}
}

func TestDetectErrors(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// Sentinel errors
	insertSym(t, db, "e1", "ErrNotFound", "var", "/repo/errors.go", "pkg/store", "", "")
	insertSym(t, db, "e2", "ErrTimeout", "var", "/repo/errors.go", "pkg/store", "", "")
	// Error-returning funcs (non-test)
	insertSym(t, db, "e3", "Open", "func", "/repo/store.go", "pkg/store", "func Open() error", "")
	insertSym(t, db, "e4", "Close", "func", "/repo/store.go", "pkg/store", "func Close() (*DB, error)", "")
	// Test file func — should be excluded
	insertSym(t, db, "e5", "TestOpen", "func", "/repo/store_test.go", "pkg/store", "func TestOpen(t *testing.T) error", "")

	result := detectErrors(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Sentinels != 2 {
		t.Errorf("sentinels: got %d, want 2", result.Sentinels)
	}
	if result.ErrorFuncs != 2 {
		t.Errorf("error_funcs: got %d, want 2", result.ErrorFuncs)
	}
	if result.Pattern != "sentinel errors" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
}

func TestDetectErrors_NoSentinels(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	insertSym(t, db, "e1", "Open", "func", "/repo/store.go", "pkg/store", "func Open() error", "")

	result := detectErrors(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Pattern != "inline error returns" {
		t.Errorf("pattern: got %q, want 'inline error returns'", result.Pattern)
	}
}

func TestDetectFileOrg(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// Single-type file
	insertSym(t, db, "f1", "Store", "struct", "/repo/store.go", "pkg/store", "", "")
	// Multi-type file
	insertSym(t, db, "f2", "Config", "struct", "/repo/types.go", "pkg/config", "", "")
	insertSym(t, db, "f3", "Options", "struct", "/repo/types.go", "pkg/config", "", "")
	insertSym(t, db, "f4", "Settings", "interface", "/repo/types.go", "pkg/config", "", "")
	// Another single-type file
	insertSym(t, db, "f5", "Index", "struct", "/repo/index.go", "pkg/index", "", "")
	// Test file — should be excluded
	insertSym(t, db, "f6", "MockStore", "struct", "/repo/store_test.go", "pkg/store", "", "")

	result := detectFileOrg(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.SingleType != 2 {
		t.Errorf("single_type: got %d, want 2", result.SingleType)
	}
	if result.MultiType != 1 {
		t.Errorf("multi_type: got %d, want 1", result.MultiType)
	}
	if result.Pattern != "one type per file" {
		t.Errorf("pattern: got %q", result.Pattern)
	}
}

func TestDetectConventions_AllCategories(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// Constructor
	insertSym(t, db, "c1", "NewStore", "func", "/repo/store.go", "pkg/store", "func NewStore() (*Store, error)", "")
	// Receiver
	insertSym(t, db, "r1", "Open", "method", "/repo/store.go", "pkg/store", "", "(*s)")
	// Test file + source file (colocated)
	insertSym(t, db, "t1", "TestOpen", "func", "/repo/store_test.go", "pkg/store", "", "")
	// Interface
	insertSym(t, db, "i1", "Reader", "interface", "/repo/io.go", "pkg/io", "", "")
	// Sentinel error
	insertSym(t, db, "e1", "ErrNotFound", "var", "/repo/errors.go", "pkg/store", "", "")
	// Struct for file org
	insertSym(t, db, "f1", "Config", "struct", "/repo/config.go", "pkg/config", "", "")

	conv := DetectConventions(db, "/repo")
	if conv == nil {
		t.Fatal("expected non-nil conventions")
	}

	count := 0
	if conv.Constructors != nil {
		count++
	}
	if conv.Receivers != nil {
		count++
	}
	if conv.Testing != nil {
		count++
	}
	if conv.Interfaces != nil {
		count++
	}
	if conv.ErrorHandling != nil {
		count++
	}
	if conv.FileOrg != nil {
		count++
	}

	if count < 4 {
		t.Errorf("expected >= 4 categories detected, got %d", count)
	}
}
