package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if s.Path() != dbPath {
		t.Errorf("Path() = %q, want %q", s.Path(), dbPath)
	}

	if s.DB() == nil {
		t.Error("DB() should not be nil")
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestMetadata(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Set and get metadata
	if err := s.SetMeta("test_key", "test_value"); err != nil {
		t.Fatalf("SetMeta failed: %v", err)
	}

	got, err := s.GetMeta("test_key")
	if err != nil {
		t.Fatalf("GetMeta failed: %v", err)
	}
	if got != "test_value" {
		t.Errorf("GetMeta() = %q, want %q", got, "test_value")
	}

	// Update existing key
	if err := s.SetMeta("test_key", "new_value"); err != nil {
		t.Fatalf("SetMeta (update) failed: %v", err)
	}

	got, err = s.GetMeta("test_key")
	if err != nil {
		t.Fatalf("GetMeta (after update) failed: %v", err)
	}
	if got != "new_value" {
		t.Errorf("GetMeta() = %q, want %q", got, "new_value")
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	if Exists(dbPath) {
		t.Error("Exists() should return false for non-existent file")
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	s.Close()

	if !Exists(dbPath) {
		t.Error("Exists() should return true for existing file")
	}
}

func TestDefaultIndexPath(t *testing.T) {
	got := DefaultIndexPath("/home/user/project")
	want := filepath.Join("/home/user/project", ".snipe", "index.db")
	if got != want {
		t.Errorf("DefaultIndexPath() = %q, want %q", got, want)
	}
}

func TestSchemaCreation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Verify tables exist by querying them
	tables := []string{"symbols", "refs", "call_graph", "meta", "files"}
	for _, table := range tables {
		var count int
		err := s.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("Table %q should exist: %v", table, err)
		}
	}

	// Verify schema version is set
	version, err := s.GetMeta("schema_version")
	if err != nil {
		t.Fatalf("GetMeta(schema_version) failed: %v", err)
	}
	if version != "11" {
		t.Errorf("schema_version = %q, want %q", version, "11")
	}
}

func TestReopenExistingDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Create and populate
	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("First Open failed: %v", err)
	}
	if err := s1.SetMeta("persistent_key", "persistent_value"); err != nil {
		t.Fatalf("SetMeta failed: %v", err)
	}
	s1.Close()

	// Reopen and verify
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Second Open failed: %v", err)
	}
	defer s2.Close()

	got, err := s2.GetMeta("persistent_key")
	if err != nil {
		t.Fatalf("GetMeta failed: %v", err)
	}
	if got != "persistent_value" {
		t.Errorf("Data not persisted: got %q, want %q", got, "persistent_value")
	}
}

func TestCreateIndexDirectory(t *testing.T) {
	dir := t.TempDir()
	nestedPath := filepath.Join(dir, "deep", "nested", "path", "test.db")

	s, err := Open(nestedPath)
	if err != nil {
		t.Fatalf("Open with nested path failed: %v", err)
	}
	defer s.Close()

	// Verify directory was created
	parentDir := filepath.Dir(nestedPath)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		t.Error("Parent directory should have been created")
	}
}

func TestConcurrentAccess(t *testing.T) {
	// Test that concurrent read/write doesn't deadlock (10 goroutines)
	// This verifies busy_timeout and connection limits work correctly
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "concurrent.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	const numGoroutines = 10
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*opsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key_%d_%d", id, j)
				value := fmt.Sprintf("value_%d_%d", id, j)

				// Write
				if err := s.SetMeta(key, value); err != nil {
					errChan <- fmt.Errorf("SetMeta failed: %w", err)
					return
				}

				// Read
				got, err := s.GetMeta(key)
				if err != nil {
					errChan <- fmt.Errorf("GetMeta failed: %w", err)
					return
				}
				if got != value {
					errChan <- fmt.Errorf("got %q, want %q", got, value)
					return
				}
			}
		}(i)
	}

	// Wait with timeout to detect deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case err := <-errChan:
		t.Fatalf("Concurrent access error: %v", err)
	}

	close(errChan)
	for err := range errChan {
		t.Errorf("Error during concurrent access: %v", err)
	}
}

func TestBusyTimeoutPragma(t *testing.T) {
	// Verify busy_timeout is set
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	var timeout int
	err = s.DB().QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if err != nil {
		t.Fatalf("PRAGMA busy_timeout query failed: %v", err)
	}

	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", timeout)
	}
}
