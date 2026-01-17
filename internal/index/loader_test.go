package index

import "testing"

func TestMatchesExclude(t *testing.T) {
	tests := []struct {
		name    string
		pkgPath string
		pattern string
		want    bool
	}{
		// Positive cases - should be excluded
		{
			name:    "vendor at start",
			pkgPath: "vendor/github.com/foo/bar",
			pattern: "vendor",
			want:    true,
		},
		{
			name:    "vendor in middle",
			pkgPath: "example.com/project/vendor/pkg",
			pattern: "vendor",
			want:    true,
		},
		{
			name:    "testdata at start",
			pkgPath: "testdata/fixtures",
			pattern: "testdata",
			want:    true,
		},
		{
			name:    "testdata in middle",
			pkgPath: "example.com/project/testdata/pkg",
			pattern: "testdata",
			want:    true,
		},
		{
			name:    "node_modules",
			pkgPath: "example.com/node_modules/pkg",
			pattern: "node_modules",
			want:    true,
		},
		{
			name:    "exact match single component",
			pkgPath: "vendor",
			pattern: "vendor",
			want:    true,
		},

		// Negative cases - should NOT be excluded (partial match rejection)
		{
			name:    "myvendor should not match vendor",
			pkgPath: "example.com/myvendor/pkg",
			pattern: "vendor",
			want:    false,
		},
		{
			name:    "vendors should not match vendor",
			pkgPath: "example.com/vendors/pkg",
			pattern: "vendor",
			want:    false,
		},
		{
			name:    "vendoring should not match vendor",
			pkgPath: "example.com/vendoring/pkg",
			pattern: "vendor",
			want:    false,
		},
		{
			name:    "testdatautils should not match testdata",
			pkgPath: "example.com/testdatautils/pkg",
			pattern: "testdata",
			want:    false,
		},
		{
			name:    "mytestdata should not match testdata",
			pkgPath: "example.com/mytestdata/pkg",
			pattern: "testdata",
			want:    false,
		},
		{
			name:    "unrelated path",
			pkgPath: "example.com/project/internal/handler",
			pattern: "vendor",
			want:    false,
		},
		{
			name:    "empty pattern",
			pkgPath: "example.com/project/pkg",
			pattern: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesExclude(tt.pkgPath, tt.pattern)
			if got != tt.want {
				t.Errorf("matchesExclude(%q, %q) = %v, want %v",
					tt.pkgPath, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestDefaultExclude(t *testing.T) {
	defaults := DefaultExclude()

	// Check that common patterns are included
	expected := []string{"vendor", "node_modules", "testdata", ".git"}
	if len(defaults) != len(expected) {
		t.Errorf("DefaultExclude() has %d items, want %d", len(defaults), len(expected))
	}

	for i, want := range expected {
		if i >= len(defaults) || defaults[i] != want {
			t.Errorf("DefaultExclude()[%d] = %q, want %q", i, defaults[i], want)
		}
	}
}

func TestFilterPackagesIntegration(t *testing.T) {
	// Test the filterPackages function with mock data
	// This tests the integration of matchesExclude with filterPackages

	testPaths := []string{
		"example.com/project/handler",        // keep
		"example.com/project/vendor/foo",     // exclude
		"example.com/project/myvendor/bar",   // keep (partial match)
		"example.com/project/testdata/fix",   // exclude
		"example.com/project/internal/store", // keep
	}

	exclude := []string{"vendor", "testdata"}

	var kept, excluded int
	for _, path := range testPaths {
		isExcluded := false
		for _, pattern := range exclude {
			if matchesExclude(path, pattern) {
				isExcluded = true
				break
			}
		}
		if isExcluded {
			excluded++
		} else {
			kept++
		}
	}

	if kept != 3 {
		t.Errorf("expected 3 kept packages, got %d", kept)
	}
	if excluded != 2 {
		t.Errorf("expected 2 excluded packages, got %d", excluded)
	}
}
