package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	results, err := Search(dir, "Hello", 10, 0)
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
	results, err := Search(dir, "NONEXISTENT_PATTERN_12345", 10, 0)
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
	_, err := Search(dir, "[invalid(regex", 10, 0)
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
	results, err := Search(dir, "MARKER", 10, 0)
	if err != nil {
		t.Fatalf("Search with long line should not fail: %v", err)
	}

	if len(results) == 0 {
		t.Error("Expected to find MARKER in long line")
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

	results, err := Search(dir, "match", 5, 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 5 {
		t.Errorf("Expected at most 5 results, got %d", len(results))
	}
}
