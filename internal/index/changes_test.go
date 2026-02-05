package index

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func storedInfoFor(t *testing.T, path string) FileInfo {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	hash, err := hashFileSHA256(path)
	if err != nil {
		t.Fatalf("hash %s: %v", path, err)
	}
	return FileInfo{
		Path:  path,
		Mtime: stat.ModTime().Unix(),
		Hash:  hash,
	}
}

func TestDetectChanges_NoStoredFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(dir, "util.go"), "package main\n")

	result, err := DetectChanges(dir, nil, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if !result.HasChanges {
		t.Error("expected HasChanges=true for empty stored files")
	}
	if len(result.Added) != 2 {
		t.Errorf("expected 2 added files, got %d", len(result.Added))
	}
	if result.Unchanged != 0 {
		t.Errorf("expected 0 unchanged, got %d", result.Unchanged)
	}
}

func TestDetectChanges_AllUnchanged(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	utilPath := filepath.Join(dir, "util.go")
	writeTestFile(t, mainPath, "package main\n")
	writeTestFile(t, utilPath, "package main\n")

	stored := map[string]FileInfo{
		mainPath: storedInfoFor(t, mainPath),
		utilPath: storedInfoFor(t, utilPath),
	}

	result, err := DetectChanges(dir, stored, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if result.HasChanges {
		t.Errorf("expected no changes, got: %s", result.Summary())
	}
	if result.Unchanged != 2 {
		t.Errorf("expected 2 unchanged, got %d", result.Unchanged)
	}
}

func TestDetectChanges_FileModified(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	writeTestFile(t, mainPath, "package main\n\nfunc hello() {}\n")

	// Simulate a prior index with an older mtime and different hash
	stored := map[string]FileInfo{
		mainPath: {
			Path:  mainPath,
			Mtime: 1000, // guaranteed mismatch with current mtime
			Hash:  "0000000000000000",
		},
	}

	result, err := DetectChanges(dir, stored, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if !result.HasChanges {
		t.Error("expected HasChanges=true after modification")
	}
	if len(result.Modified) != 1 {
		t.Errorf("expected 1 modified, got %d", len(result.Modified))
	}
}

func TestDetectChanges_FileAdded(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	writeTestFile(t, mainPath, "package main\n")

	stored := map[string]FileInfo{
		mainPath: storedInfoFor(t, mainPath),
	}

	// Add a new file
	newPath := filepath.Join(dir, "new.go")
	writeTestFile(t, newPath, "package main\n")

	result, err := DetectChanges(dir, stored, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if !result.HasChanges {
		t.Error("expected HasChanges=true after adding file")
	}
	if len(result.Added) != 1 {
		t.Errorf("expected 1 added, got %d", len(result.Added))
	}
	if result.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", result.Unchanged)
	}
}

func TestDetectChanges_FileDeleted(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	utilPath := filepath.Join(dir, "util.go")
	writeTestFile(t, mainPath, "package main\n")
	writeTestFile(t, utilPath, "package main\n")

	stored := map[string]FileInfo{
		mainPath: storedInfoFor(t, mainPath),
		utilPath: storedInfoFor(t, utilPath),
	}

	// Delete one file
	_ = os.Remove(utilPath)

	result, err := DetectChanges(dir, stored, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if !result.HasChanges {
		t.Error("expected HasChanges=true after deleting file")
	}
	if len(result.Deleted) != 1 {
		t.Errorf("expected 1 deleted, got %d", len(result.Deleted))
	}
	if result.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", result.Unchanged)
	}
}

func TestDetectChanges_MtimeChangedContentSame(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	content := "package main\n"
	writeTestFile(t, mainPath, content)

	// Store the correct content hash but with an older mtime to force hash comparison
	info := storedInfoFor(t, mainPath)
	info.Mtime = 1000 // older mtime forces hash check
	stored := map[string]FileInfo{
		mainPath: info,
	}

	result, err := DetectChanges(dir, stored, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if result.HasChanges {
		t.Error("expected no changes when content hash matches despite mtime change")
	}
	if result.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", result.Unchanged)
	}
}

func TestDetectChanges_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(dir, ".git", "config.go"), "package git\n")
	writeTestFile(t, filepath.Join(dir, ".snipe", "internal.go"), "package snipe\n")

	result, err := DetectChanges(dir, nil, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	// Should only find main.go, not files in hidden dirs
	if len(result.Added) != 1 {
		t.Errorf("expected 1 added (main.go only), got %d: %v", len(result.Added), result.Added)
	}
}

func TestDetectChanges_SkipsExcludedDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(dir, "vendor", "lib.go"), "package lib\n")
	writeTestFile(t, filepath.Join(dir, "node_modules", "mod.go"), "package mod\n")

	result, err := DetectChanges(dir, nil, DefaultExclude())
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if len(result.Added) != 1 {
		t.Errorf("expected 1 added (main.go only), got %d: %v", len(result.Added), result.Added)
	}
}

func TestDetectChanges_IgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(dir, "readme.md"), "# readme\n")
	writeTestFile(t, filepath.Join(dir, "data.json"), "{}\n")

	result, err := DetectChanges(dir, nil, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if len(result.Added) != 1 {
		t.Errorf("expected 1 added (.go files only), got %d: %v", len(result.Added), result.Added)
	}
}

func TestDetectChanges_Subdirectories(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n")
	writeTestFile(t, filepath.Join(dir, "internal", "pkg", "lib.go"), "package pkg\n")

	result, err := DetectChanges(dir, nil, nil)
	if err != nil {
		t.Fatalf("DetectChanges: %v", err)
	}

	if len(result.Added) != 2 {
		t.Errorf("expected 2 added (recursive), got %d: %v", len(result.Added), result.Added)
	}
}

func TestChangeResult_Summary(t *testing.T) {
	tests := []struct {
		name   string
		cr     ChangeResult
		expect string
	}{
		{
			name:   "no changes",
			cr:     ChangeResult{HasChanges: false, Unchanged: 42},
			expect: "no changes (42 files)",
		},
		{
			name:   "modified only",
			cr:     ChangeResult{HasChanges: true, Modified: []string{"a.go"}, Unchanged: 10},
			expect: "1 modified, 10 unchanged",
		},
		{
			name:   "mixed changes",
			cr:     ChangeResult{HasChanges: true, Modified: []string{"a.go"}, Added: []string{"b.go", "c.go"}, Deleted: []string{"d.go"}, Unchanged: 5},
			expect: "1 modified, 2 added, 1 deleted, 5 unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cr.Summary()
			if got != tt.expect {
				t.Errorf("Summary() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestChangeResult_TotalChanged(t *testing.T) {
	cr := ChangeResult{
		Modified: []string{"a.go", "b.go"},
		Added:    []string{"c.go"},
		Deleted:  []string{"d.go"},
	}
	if got := cr.TotalChanged(); got != 4 {
		t.Errorf("TotalChanged() = %d, want 4", got)
	}
}
