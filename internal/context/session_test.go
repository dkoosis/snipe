package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionRecordQuery(t *testing.T) {
	// Create a temp directory for testing
	tmpDir, err := os.MkdirTemp("", "snipe-session-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	session := &Session{Project: tmpDir}

	// Record some queries
	session.RecordQuery("Foo", "pkg/foo.go", 10, "func", "def")
	session.RecordQuery("Bar", "pkg/bar.go", 20, "type", "refs")
	session.RecordQuery("Baz", "pkg/baz.go", 30, "method", "callers")

	if len(session.Queries) != 3 {
		t.Errorf("expected 3 queries, got %d", len(session.Queries))
	}

	// Most recent should be first
	if session.Queries[0].Symbol != "Baz" {
		t.Errorf("expected most recent query to be Baz, got %s", session.Queries[0].Symbol)
	}

	// Record duplicate query - should update, not add
	session.RecordQuery("Foo", "pkg/foo.go", 10, "func", "def")
	if len(session.Queries) != 3 {
		t.Errorf("expected 3 queries after duplicate, got %d", len(session.Queries))
	}

	// Foo should now be first (most recent)
	if session.Queries[0].Symbol != "Foo" {
		t.Errorf("expected Foo to be first after re-query, got %s", session.Queries[0].Symbol)
	}
}

func TestSessionActiveWork(t *testing.T) {
	session := &Session{
		Project: "/test",
		Branch:  "main",
	}

	// Empty session should return nil
	aw := session.GetActiveWork()
	if aw != nil {
		t.Error("expected nil active work for empty session")
	}

	// Add some queries
	session.RecordQuery("Foo", "foo.go", 1, "func", "def")
	session.RecordQuery("Bar", "bar.go", 2, "type", "refs")

	aw = session.GetActiveWork()
	if aw == nil {
		t.Fatal("expected active work, got nil")
		return
	}

	if len(aw.RecentSymbols) != 2 {
		t.Errorf("expected 2 recent symbols, got %d", len(aw.RecentSymbols))
	}

	// First symbol should be most recent (Bar)
	if aw.RecentSymbols[0].Name != "Bar" {
		t.Errorf("expected Bar first, got %s", aw.RecentSymbols[0].Name)
	}
}

func TestSessionPersistence(t *testing.T) {
	// Create a temp directory for testing
	tmpDir, err := os.MkdirTemp("", "snipe-session-persist")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create session and add queries
	session := &Session{
		Project: tmpDir,
		Branch:  "test-branch",
	}
	session.RecordQuery("Foo", "foo.go", 10, "func", "def")

	// Save
	if err := SaveSession(session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Check file exists
	sessionPath := filepath.Join(tmpDir, ".snipe", "session.json")
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		t.Error("session file not created")
	}

	// Load and verify
	loaded, err := LoadSession(tmpDir)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}

	if len(loaded.Queries) != 1 {
		t.Errorf("expected 1 query, got %d", len(loaded.Queries))
	}

	if loaded.Queries[0].Symbol != "Foo" {
		t.Errorf("expected Foo, got %s", loaded.Queries[0].Symbol)
	}
}

func TestSessionMaxQueries(t *testing.T) {
	session := &Session{Project: "/test"}

	// Add more than maxQueries
	for i := 0; i < 30; i++ {
		session.RecordQuery("Sym"+string(rune('A'+i)), "file.go", i, "func", "def")
	}

	if len(session.Queries) > maxQueries {
		t.Errorf("expected max %d queries, got %d", maxQueries, len(session.Queries))
	}
}
