package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferPurpose(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"cmd", "Command-line interface"},
		{"internal", "Internal implementation packages"},
		{"store", "Data storage and persistence"},
		{"query", "Query execution"},
		{"unknown", "Application logic"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			got := inferPurpose(tt.dir)
			if got != tt.want {
				t.Errorf("inferPurpose(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestCategorizeByConcern(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"cmd/root.go", "cli"},
		{"internal/store/store.go", "storage"},
		{"internal/query/lookup.go", "query"},
		{"internal/index/refs.go", "indexing"},
		{"internal/output/types.go", "output"},
		{"test/bench/bench_test.go", "testing"},
		{"main.go", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			got := categorizeByConcern(tt.relPath)
			if got != tt.want {
				t.Errorf("categorizeByConcern(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestInferPackagePurpose_SpecificPackages(t *testing.T) {
	tests := []struct {
		pkg  string
		want string
	}{
		{"internal/vector", "Vector math for embedding similarity"},
		{"bench", "Benchmarks and baseline capture"},
		{"test/blackbox", "Integration tests (blackbox)"},
		{"test/bench", "Benchmarks and baseline capture"},
		{"internal/util", "Shared utility functions (project root, caching)"},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			got := inferPackagePurpose(tt.pkg)
			if got != tt.want {
				t.Errorf("inferPackagePurpose(%q) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}

func TestDescribeFile(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"cmd/root.go", "CLI root command"},
		{"internal/store/store.go", "Database operations"},
		{"internal/store/schema.go", "Database schema"},
		{"internal/config/config.go", "Configuration handling"},
		{"internal/unknown/unknown.go", "Implementation"},
	}

	for _, tt := range tests {
		t.Run(tt.relPath, func(t *testing.T) {
			got := describeFile(tt.relPath)
			if got != tt.want {
				t.Errorf("describeFile(%q) = %q, want %q", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestInferProjectPurpose_Fallback(t *testing.T) {
	// With no DB, should return empty
	purpose := inferProjectPurpose(nil, "/nonexistent", "myproject")
	if purpose != "" {
		t.Errorf("expected empty for nil DB, got %q", purpose)
	}
}

func TestInferProjectPurpose_FromReadme(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(
		"# myproject\n\nA fast CLI tool for indexing Go code.\n\nMore stuff here.\n",
	), 0644)

	purpose := inferProjectPurpose(nil, dir, "myproject")
	if purpose == "" {
		t.Error("expected purpose from README, got empty")
	}
	if purpose != "A fast CLI tool for indexing Go code." {
		t.Errorf("purpose = %q, want %q", purpose, "A fast CLI tool for indexing Go code.")
	}
}
