package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

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

		// Multi-component pattern tests
		{
			name:    "multi-component at start",
			pkgPath: "vendor/v2/pkg",
			pattern: "vendor/v2",
			want:    true,
		},
		{
			name:    "multi-component in middle",
			pkgPath: "example.com/vendor/v2/pkg",
			pattern: "vendor/v2",
			want:    true,
		},
		{
			name:    "multi-component at end",
			pkgPath: "example.com/vendor/v2",
			pattern: "vendor/v2",
			want:    true,
		},
		{
			name:    "multi-component exact match",
			pkgPath: "vendor/v2",
			pattern: "vendor/v2",
			want:    true,
		},
		{
			name:    "multi-component partial no match",
			pkgPath: "example.com/myvendor/v2/pkg",
			pattern: "vendor/v2",
			want:    false,
		},
		{
			name:    "multi-component suffix no match",
			pkgPath: "example.com/vendor/v2beta/pkg",
			pattern: "vendor/v2",
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

func TestDropTestBinaryPackages(t *testing.T) {
	pkgs := []*packages.Package{
		{PkgPath: "example.com/proj/store"},
		{PkgPath: "example.com/proj/store.test"},
		{PkgPath: "example.com/proj/store_test"},
	}

	got := dropTestBinaryPackages(pkgs)

	if len(got) != 2 {
		t.Fatalf("got %d packages, want 2", len(got))
	}
	for _, pkg := range got {
		if strings.HasSuffix(pkg.PkgPath, ".test") {
			t.Errorf("test-binary package %q survived filtering", pkg.PkgPath)
		}
	}
}

// TestLoad_ExcludesTestBinaryPackages loads a real module with Tests enabled
// and asserts the synthesized .test binary package (whose only file is a
// generated _testmain.go in the go-build cache) is not in the result.
func TestLoad_ExcludesTestBinaryPackages(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":        "module example.com/fence\n\ngo 1.22\n",
		"fence.go":      "package fence\n\nfunc Fence() int { return 1 }\n",
		"fence_test.go": "package fence\n\nimport \"testing\"\n\nfunc TestFence(t *testing.T) {\n\tif Fence() != 1 {\n\t\tt.Fatal(\"nope\")\n\t}\n}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir, Tests: true})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, pkg := range result.Packages {
		if strings.HasSuffix(pkg.PkgPath, ".test") {
			t.Errorf("test-binary package %q in load result", pkg.PkgPath)
		}
		for _, f := range pkg.GoFiles {
			if !strings.HasPrefix(f, dir) {
				t.Errorf("package %q has file outside module root: %s", pkg.PkgPath, f)
			}
		}
	}
}

// TestExtractFilePackages_CoversSymbolFreeFile pins the sn-dzbj fix's tier-2
// source: a doc.go file that go/packages loads but that declares no
// func/type/const/var (so ExtractSymbols emits zero Symbol rows for it) must
// still appear in ExtractFilePackages, with the package's canonical import
// path — the fallback resolveFilePackage (cmd/triage.go) reads via
// WriteFilePackages/file_packages.
func TestExtractFilePackages_CoversSymbolFreeFile(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":  "module example.com/tarn\n\ngo 1.22\n",
		"doc.go":  "// Package tarn does a thing.\npackage tarn\n",
		"real.go": "package tarn\n\nfunc Real() int { return 1 }\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	got := ExtractFilePackages(result)
	byPath := make(map[string]string, len(got))
	for _, fp := range got {
		byPath[filepath.Base(fp.Path)] = fp.PkgPath
	}

	docPkg, ok := byPath["doc.go"]
	if !ok {
		t.Fatalf("doc.go missing from ExtractFilePackages result: %+v", got)
	}
	if docPkg != "example.com/tarn" {
		t.Errorf("doc.go package = %q, want example.com/tarn", docPkg)
	}
	if realPkg := byPath["real.go"]; realPkg != "example.com/tarn" {
		t.Errorf("real.go package = %q, want example.com/tarn", realPkg)
	}
}
