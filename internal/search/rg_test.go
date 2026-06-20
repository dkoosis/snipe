package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSearch_BasicMatch(t *testing.T) {
	// Create a temp directory with a test file
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.go")
	content := `package main

func Hello() {
	println("Hello, World!")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	results, err := Search(context.Background(), dir, "Hello", 10, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected at least one match")
	}
}

func TestSearch_NoMatches(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.go")
	content := `package main

func Foo() {
	println("bar")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Search for nonexistent pattern returns empty results, not error
	results, err := Search(context.Background(), dir, "NONEXISTENT_PATTERN_12345", 10, 0)
	if err != nil {
		t.Errorf("Search with no matches should not return error, got: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected empty results for no matches, got %d results", len(results))
	}
}

func TestSearch_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Invalid regex should return an error
	_, err := Search(context.Background(), dir, "[invalid(regex", 10, 0)
	if err == nil {
		t.Error("Search with invalid regex should return error")
	}
}

func TestSearch_LongLine(t *testing.T) {
	// Test that file with 500KB line doesn't panic (scanner buffer test)
	dir := t.TempDir()
	testFile := filepath.Join(dir, "longline.txt")

	// Create a line with ~500KB of content
	longContent := "MARKER" + strings.Repeat("x", 500*1024) + "\n"
	if err := os.WriteFile(testFile, []byte(longContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// This should not panic due to scanner buffer limits
	results, err := Search(context.Background(), dir, "MARKER", 10, 0)
	if err != nil {
		t.Fatalf("Search with long line should not fail: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected to find MARKER in long line")
	}
}

func TestSearch_StderrCapture(t *testing.T) {
	// Search with invalid regex should include rg's error message
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := Search(context.Background(), dir, "[invalid(regex", 10, 0)
	if err == nil {
		t.Fatal("Expected error for invalid regex")
	}

	errMsg := err.Error()
	// rg exit code 2 should include stderr content
	if !strings.Contains(errMsg, "rg error") && !strings.Contains(errMsg, "rg failed") {
		t.Errorf("Error should mention rg error, got: %s", errMsg)
	}
}

func TestSearch_Limit(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	// Create file with many matches
	content := strings.Repeat("match\n", 100)
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	results, err := Search(context.Background(), dir, "match", 5, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 5 {
		t.Errorf("Expected at most 5 results, got %d", len(results))
	}
}

// TestSearch_ContextCancelled asserts that a context cancelled before/at the
// start of the search kills rg and makes Search return promptly instead of
// running the (potentially pathological) pattern to completion. Regression for
// snipe-4gz: Search used exec.Command and ignored the context entirely.
func TestSearch_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	// A tree large enough that an unbounded rg over it would take real time.
	// With a cancelled context, exec.CommandContext kills rg immediately, so
	// Search must return well under the deadline below.
	body := strings.Repeat("aaaaaaaaaaaaaaaaaaaaaaaa\n", 2000)
	for i := 0; i < 200; i++ {
		f := filepath.Join(dir, "file"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(f, []byte(body), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Search runs — rg should never run to completion

	done := make(chan struct{})
	var searchErr error
	go func() {
		// Catastrophic-backtrack-shaped pattern; would be slow if rg ran fully.
		_, searchErr = Search(ctx, dir, "(a+)+$", 50, 0)
		close(done)
	}()

	select {
	case <-done:
		// Returned promptly. With a cancelled context rg is killed before it
		// produces output, so we expect an error (process killed / no results),
		// not a hang. The exact error is platform-dependent; assert non-hang.
		if searchErr == nil {
			t.Log("Search returned nil error on cancelled context (rg finished before signal); acceptable as long as it returned promptly")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Search did not return promptly after context cancellation — rg was not killed (snipe-4gz regression)")
	}
}

// TestSearch_ContextDeadline asserts that an already-expired deadline bounds the
// rg subprocess: Search returns promptly rather than blocking on a long scan.
func TestSearch_ContextDeadline(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("aaaaaaaaaaaaaaaaaaaaaaaa\n", 2000)
	for i := 0; i < 200; i++ {
		f := filepath.Join(dir, "file"+strconv.Itoa(i)+".txt")
		if err := os.WriteFile(f, []byte(body), 0644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	done := make(chan struct{})
	go func() {
		_, _ = Search(ctx, dir, "(a+)+$", 50, 0)
		close(done)
	}()

	select {
	case <-done:
		// Returned promptly under an expired deadline.
	case <-time.After(10 * time.Second):
		t.Fatal("Search did not honor an expired context deadline (snipe-4gz regression)")
	}
}

func TestWithinRoot(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		name string
		rel  string
		err  error
		want bool
	}{
		{"inside", "cmd" + sep + "search.go", nil, true},
		{"same dir file", "search.go", nil, true},
		{"root itself", ".", nil, true},
		{"parent", "..", nil, false},
		{"escapes to cache", ".." + sep + ".." + sep + "Library" + sep + "Caches" + sep + "go-build" + sep + "x", nil, false},
		{"rel error", "anything", errors.New("Rel failed"), false},
		// A dir named "..foo" must NOT be treated as an escape.
		{"dotdot prefix not separator", "..foo" + sep + "x.go", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := withinRoot(tc.rel, tc.err); got != tc.want {
				t.Errorf("withinRoot(%q, %v) = %v, want %v", tc.rel, tc.err, got, tc.want)
			}
		})
	}
}
