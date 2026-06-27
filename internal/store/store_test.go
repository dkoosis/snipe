package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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
	tables := []string{"symbols", "refs", "call_graph", "meta", "files", "package_docs"}
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
	if version != "18" {
		t.Errorf("schema_version = %q, want %q", version, "18")
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
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock detected: concurrent access test timed out")
	}

	close(errChan)
	for err := range errChan {
		t.Errorf("Error during concurrent access: %v", err)
	}
}

func TestAcquireLockTwiceFails(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("First AcquireLock failed: %v", err)
	}
	defer ReleaseLock(dbPath)

	if err := AcquireLock(dbPath); err == nil {
		t.Fatal("Second AcquireLock should fail, but got nil error")
	}
}

func TestAcquireLockWritesPID(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer ReleaseLock(dbPath)

	data, err := os.ReadFile(LockPath(dbPath))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	want := fmt.Sprintf("%d\n", os.Getpid())
	if string(data) != want {
		t.Errorf("lock file content = %q, want %q", string(data), want)
	}
}

func TestAcquireLockRecoversStaleLock(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)

	// Create a lock file with a dead PID (PID 1 is init, but use a very
	// high PID that almost certainly doesn't exist)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("9999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Should succeed by detecting and removing the stale lock
	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("AcquireLock should recover stale lock, got: %v", err)
	}
	defer ReleaseLock(dbPath)
}

func TestAcquireLockRecoversEmptyLockFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)

	// Create an empty lock file (old format or crash mid-write)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	// Should succeed by treating empty file as stale
	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("AcquireLock should recover empty lock file, got: %v", err)
	}
	defer ReleaseLock(dbPath)
}

// TestIsIndexingFalseWhenNoLock confirms a missing lock file reports not-indexing.
func TestIsIndexingFalseWhenNoLock(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	if IsIndexing(dbPath) {
		t.Fatal("IsIndexing should be false when no lock file exists")
	}
}

// TestIsIndexingTrueWhenLockAlive confirms a live lock holder reports in-progress.
func TestIsIndexingTrueWhenLockAlive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	defer ReleaseLock(dbPath)
	if !IsIndexing(dbPath) {
		t.Fatal("IsIndexing should be true while this process holds the lock")
	}
}

// TestIsIndexingFalseWhenLockStale is the regression test for the bug where an
// interrupted index left a lock with a dead PID, gating every read command
// (diagram, etc.) even though the index was fresh. IsIndexing must detect the
// dead holder, remove the stale lock, and report not-indexing.
func TestIsIndexingFalseWhenLockStale(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)

	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	// Dead PID: a very high value that almost certainly isn't running.
	if err := os.WriteFile(lockPath, []byte("9999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if IsIndexing(dbPath) {
		t.Fatal("IsIndexing should be false when lock holder is dead")
	}
	// And the stale lock should have been cleaned up.
	if Exists(lockPath) {
		t.Fatal("IsIndexing should have removed the stale lock file")
	}
}

// TestRemoveLockIfContentsMatch_RefusesWhenContentsChanged simulates the TOCTOU
// scenario where, between a staleness verdict on a dead PID and the unlink, a
// fresh process re-locks. The match check must refuse to remove the live lock.
func TestRemoveLockIfContentsMatch_RefusesWhenContentsChanged(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	// Initial: dead PID we'd flag stale.
	if err := os.WriteFile(lockPath, []byte("9999999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Race: another process re-locks before our remove.
	if err := os.WriteFile(lockPath, []byte("1234\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if removeLockIfContentsMatch(lockPath, "9999999") {
		t.Fatal("expected refusal to remove lock whose contents changed")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock should still exist after refusal, stat err: %v", err)
	}
}

func TestRemoveLockIfContentsMatch_RemovesWhenContentsMatch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("9999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if !removeLockIfContentsMatch(lockPath, "9999999") {
		t.Fatal("expected removal when contents match")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock should be gone, stat err: %v", err)
	}
}

// TestReleaseLockLeavesForeignLock is the regression test for snipe-dv3: after a
// stale-reap race, process A's deferred ReleaseLock must NOT unlink a lock that
// now holds process B's (foreign) PID. Release is ownership-checked just like
// acquire — it only removes a lock whose contents are THIS process's PID.
func TestReleaseLockLeavesForeignLock(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		t.Fatal(err)
	}
	// A live foreign holder. Use a PID that is definitely not us; the value
	// need not be a running process — ReleaseLock keys on contents, not liveness.
	foreign := os.Getpid() + 1
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("%d\n", foreign)), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ReleaseLock(dbPath); err != nil {
		t.Fatalf("ReleaseLock should not error when refusing a foreign lock, got: %v", err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("foreign lock must survive ReleaseLock, stat err: %v", err)
	}
}

// TestReleaseLockRemovesOwnLock confirms the happy path: a lock holding THIS
// process's PID is removed and ReleaseLock reports no error.
func TestReleaseLockRemovesOwnLock(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	lockPath := LockPath(dbPath)

	if err := AcquireLock(dbPath); err != nil {
		t.Fatalf("AcquireLock failed: %v", err)
	}
	if err := ReleaseLock(dbPath); err != nil {
		t.Fatalf("ReleaseLock failed for own lock: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("own lock should be gone after ReleaseLock, stat err: %v", err)
	}
}

func TestOpenSetsPragmas(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "pragmas.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	var mode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode query failed: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	var synchronous int
	if err := s.DB().QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("PRAGMA synchronous query failed: %v", err)
	}
	if synchronous != 1 {
		t.Errorf("synchronous = %d, want 1", synchronous)
	}

	var tempStore int
	if err := s.DB().QueryRow("PRAGMA temp_store").Scan(&tempStore); err != nil {
		t.Fatalf("PRAGMA temp_store query failed: %v", err)
	}
	if tempStore != 2 {
		t.Errorf("temp_store = %d, want 2", tempStore)
	}

	var foreignKeys int
	if err := s.DB().QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("PRAGMA foreign_keys query failed: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
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
