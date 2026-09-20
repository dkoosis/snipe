package index_test

import (
	"go/ast"
	"os"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/dkoosis/snipe/internal/index"
)

func pkgDocFile(text string) *ast.File {
	return &ast.File{Doc: &ast.CommentGroup{List: []*ast.Comment{{Text: "// " + text}}}}
}

func TestExtractPackageDocs_SkipsTestFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		docs  []string
		want  string // "" = package yields no doc
	}{
		{"test-only comment is ignored", []string{"/p/p_test.go"}, []string{"Package p verifies storage."}, ""},
		{"non-test comment wins over test comment", []string{"/p/p_test.go", "/p/p.go"}, []string{"Package p verifies storage.", "Package p stores things."}, "Package p stores things."},
		{"doc.go still wins", []string{"/p/a.go", "/p/doc.go"}, []string{"Package p is a.", "Package p is the doc."}, "Package p is the doc."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := &packages.Package{PkgPath: "example.com/p", GoFiles: tt.files}
			for _, d := range tt.docs {
				pkg.Syntax = append(pkg.Syntax, pkgDocFile(d))
			}
			docs := index.ExtractPackageDocs(&index.LoadResult{Packages: []*packages.Package{pkg}})
			got := ""
			if len(docs) == 1 {
				got = docs[0].Doc
			}
			if got != tt.want {
				t.Errorf("doc = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractPackageDocs_NonEmpty(t *testing.T) {
	// Use the project root so we pick up packages that have doc comments.
	// Fall back to "." if we can't find the root.
	dir := "."
	if wd, err := os.Getwd(); err == nil {
		// We're in internal/index, go up two levels to project root
		root := wd + "/../.."
		if _, err := os.Stat(root + "/go.mod"); err == nil {
			dir = root
		}
	}

	result, err := index.Load(index.LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	docs := index.ExtractPackageDocs(result)
	if len(docs) == 0 {
		t.Skip("no package docs found (no packages with doc comments in tree)")
	}
	for _, d := range docs {
		if d.PkgPath == "" {
			t.Error("empty PkgPath in PackageDoc")
		}
		if d.Doc == "" {
			t.Error("empty Doc in PackageDoc")
		}
	}
}
