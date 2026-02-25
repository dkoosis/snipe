package query

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

const testMainGo = "main.go"

func TestCheckFileStaleness_FreshFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	// Create a real file with known mtime
	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	// Open store and write file metadata with matching mtime
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: info.ModTime().Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 0 {
		t.Errorf("expected no stale files, got %v", stale)
	}
}

func TestCheckFileStaleness_StaleFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	// Create a real file
	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	// Open store and write file metadata with old mtime
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store an mtime older than the file's actual mtime
	oldMtime := time.Now().Add(-1 * time.Hour).Unix()
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: oldMtime},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file, got %v", stale)
	}
	if stale[0] != testMainGo {
		t.Errorf("expected relative path 'main.go', got %q", stale[0])
	}
}

func TestCheckFileStaleness_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	deletedFile := filepath.Join(dir, "deleted.go")

	// Open store and write metadata for a file that doesn't exist on disk
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.WriteFiles([]index.FileInfo{
		{Path: deletedFile, Mtime: time.Now().Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: deletedFile, File: "deleted.go"},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file (deleted), got %v", stale)
	}
	if stale[0] != "deleted.go" {
		t.Errorf("expected 'deleted.go', got %q", stale[0])
	}
}

func TestCheckFileStaleness_EmptyResults(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	stale := CheckFileStaleness(s.DB(), dir, nil)
	if stale != nil {
		t.Errorf("expected nil for empty results, got %v", stale)
	}

	stale = CheckFileStaleness(s.DB(), dir, []output.Result{})
	if stale != nil {
		t.Errorf("expected nil for zero-length results, got %v", stale)
	}
}

func TestCheckFileStaleness_MixedFreshAndStale(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	// Create two files
	freshFile := filepath.Join(dir, "fresh.go")
	staleFile := filepath.Join(dir, "stale.go")
	for _, f := range []string{freshFile, staleFile} {
		if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	freshInfo, _ := os.Stat(freshFile)

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.WriteFiles([]index.FileInfo{
		{Path: freshFile, Mtime: freshInfo.ModTime().Unix()},
		{Path: staleFile, Mtime: time.Now().Add(-1 * time.Hour).Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: freshFile, File: "fresh.go"},
		{FileAbs: staleFile, File: "stale.go"},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file, got %v", stale)
	}
	if stale[0] != "stale.go" {
		t.Errorf("expected 'stale.go', got %q", stale[0])
	}
}

func TestCheckFileStaleness_EmptyFileAbs(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Results with empty FileAbs should be skipped
	results := []output.Result{
		{FileAbs: "", File: "something.go"},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if stale != nil {
		t.Errorf("expected nil when all FileAbs empty, got %v", stale)
	}
}

func TestCheckFileStaleness_FileNotInIndex(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	// Create a file that exists on disk but is not in the index
	unknownFile := filepath.Join(dir, "unknown.go")
	if err := os.WriteFile(unknownFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Don't write any file metadata — file is "not in index"
	results := []output.Result{
		{FileAbs: unknownFile, File: "unknown.go"},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file (not in index), got %v", stale)
	}
}

func TestCheckPathStaleness(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store old mtime
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: time.Now().Add(-1 * time.Hour).Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	stale := CheckPathStaleness(s.DB(), dir, []string{testFile})
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale path, got %v", stale)
	}
	if stale[0] != testMainGo {
		t.Errorf("expected 'main.go', got %q", stale[0])
	}

	// Empty paths
	stale = CheckPathStaleness(s.DB(), dir, nil)
	if stale != nil {
		t.Errorf("expected nil for empty paths, got %v", stale)
	}
}

func TestCheckFileStaleness_Sorted(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	// Create files
	files := []string{"c.go", "a.go", "b.go"}
	for _, name := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("package main"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// All stale (old mtimes)
	oldMtime := time.Now().Add(-1 * time.Hour).Unix()
	fileInfos := make([]index.FileInfo, len(files))
	var results []output.Result
	for i, name := range files {
		abs := filepath.Join(dir, name)
		fileInfos[i] = index.FileInfo{Path: abs, Mtime: oldMtime}
		results = append(results, output.Result{FileAbs: abs, File: name})
	}
	if err := s.WriteFiles(fileInfos); err != nil {
		t.Fatal(err)
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 3 {
		t.Fatalf("expected 3 stale files, got %v", stale)
	}
	// Should be sorted
	if stale[0] != "a.go" || stale[1] != "b.go" || stale[2] != "c.go" {
		t.Errorf("expected sorted [a.go b.go c.go], got %v", stale)
	}
}

func TestCheckFileStaleness_HashMatch_MtimeChanged(t *testing.T) {
	// Key scenario: mtime changed (e.g. git checkout, build tool touch) but
	// content is identical — should be treated as fresh.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	content := []byte("package main\n\nfunc main() {}\n")
	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Compute the real hash of this content
	hash, err := index.HashFileSHA256(testFile)
	if err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store with old mtime but correct hash
	oldMtime := time.Now().Add(-1 * time.Hour).Unix()
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: oldMtime, Hash: hash},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 0 {
		t.Errorf("expected fresh (same content, different mtime), got stale: %v", stale)
	}
}

func TestCheckFileStaleness_HashMismatch_MtimeChanged(t *testing.T) {
	// Content actually changed and mtime changed — should be stale.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store with old mtime and a hash that won't match current content
	oldMtime := time.Now().Add(-1 * time.Hour).Unix()
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: oldMtime, Hash: "deadbeefdeadbeef"},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file (hash mismatch), got %v", stale)
	}
	if stale[0] != testMainGo {
		t.Errorf("expected 'main.go', got %q", stale[0])
	}
}

func TestCheckFileStaleness_NoHash_FallsBackToMtime(t *testing.T) {
	// When no hash is stored, mtime-only comparison should still work.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store with old mtime, no hash (empty string)
	oldMtime := time.Now().Add(-1 * time.Hour).Unix()
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: oldMtime, Hash: ""},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	// Should be stale — no hash to save it, mtime is newer
	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale file (no hash, mtime fallback), got %v", stale)
	}
	if stale[0] != testMainGo {
		t.Errorf("expected 'main.go', got %q", stale[0])
	}
}

func TestCheckFileStaleness_HashMatch_MtimeFresh(t *testing.T) {
	// When mtime hasn't changed, hash check should not even be needed — still fresh.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, ".snipe", "index.db")

	testFile := filepath.Join(dir, testMainGo)
	if err := os.WriteFile(testFile, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Store with matching mtime and a hash
	hash, err := index.HashFileSHA256(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFiles([]index.FileInfo{
		{Path: testFile, Mtime: info.ModTime().Unix(), Hash: hash},
	}); err != nil {
		t.Fatal(err)
	}

	results := []output.Result{
		{FileAbs: testFile, File: testMainGo},
	}

	stale := CheckFileStaleness(s.DB(), dir, results)
	if len(stale) != 0 {
		t.Errorf("expected fresh (mtime matches), got stale: %v", stale)
	}
}
