package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePatterns_WithGoWork_ReturnsPerModulePatterns(t *testing.T) {
	dir := t.TempDir()
	goWork := `go 1.21

use (
	.
	./graph
	./store
	./mcp
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		"./...":       true,
		"./graph/...": true,
		"./store/...": true,
		"./mcp/...":   true,
	}
	if len(patterns) != len(want) {
		t.Fatalf("got %d patterns, want %d: %v", len(patterns), len(want), patterns)
	}
	for _, p := range patterns {
		if !want[p] {
			t.Errorf("unexpected pattern: %s", p)
		}
	}
}

func TestWorkspacePatterns_NoGoWork_ReturnsDotSlashDotDotDot(t *testing.T) {
	dir := t.TempDir()

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./..." {
		t.Fatalf("got %v, want [./...]", patterns)
	}
}

func TestWorkspacePatterns_MalformedGoWork_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte("not a valid go.work file {{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := WorkspacePatterns(dir)
	if err == nil {
		t.Fatal("expected error for malformed go.work")
	}
}

func TestWorkspacePatterns_EmptyUseDirectives_ReturnsDotSlashDotDotDot(t *testing.T) {
	dir := t.TempDir()
	goWork := "go 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./..." {
		t.Fatalf("got %v, want [./...]", patterns)
	}
}

func TestWorkspacePatterns_NonexistentUseDir_StillReturnsPattern(t *testing.T) {
	dir := t.TempDir()
	goWork := "go 1.21\n\nuse ./doesnotexist\n"
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./doesnotexist/..." {
		t.Fatalf("got %v, want [./doesnotexist/...]", patterns)
	}
}
