package context

import (
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
