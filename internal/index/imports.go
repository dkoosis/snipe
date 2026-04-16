package index

import (
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Import represents a package import in a file
type Import struct {
	FilePath    string // Importing file
	PkgPath     string // Imported package path (e.g., "fmt", "github.com/foo/bar")
	Name        string // Local name if aliased, empty otherwise
	Line        int    // Line number of import statement
	Col         int    // Column of import statement
	ImporterPkg string // Package path of the importing file
}

// ExtractImports extracts all imports from loaded packages
func ExtractImports(result *LoadResult) ([]Import, error) {
	return ExtractImportsFiltered(result, nil)
}

// ExtractImportsFiltered extracts imports, optionally limited to specific files.
// When onlyFiles is non-nil, only imports from those files are extracted.
//
// Deduplication: go/packages with Tests=true loads two variants of each package
// containing non-test files (the regular package and the test-augmented variant).
// This causes every non-test file to be visited twice. We deduplicate by
// (file_path, line, col) so each import appears exactly once per file.
func ExtractImportsFiltered(result *LoadResult, onlyFiles map[string]bool) ([]Import, error) {
	var imports []Import
	seen := make(map[string]struct{})

	for _, pkg := range result.Packages {
		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]

			// Skip files not in the filter set (if filtering)
			if onlyFiles != nil && !onlyFiles[filePath] {
				continue
			}

			for _, imp := range extractFileImports(pkg, file, filePath, result) {
				key := fmt.Sprintf("%s|%d|%d|%s", imp.FilePath, imp.Line, imp.Col, imp.PkgPath)
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				imports = append(imports, imp)
			}
		}
	}

	return imports, nil
}

func extractFileImports(pkg *packages.Package, file *ast.File, filePath string, result *LoadResult) []Import {
	var imports []Import

	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}

		// Get import path (strip quotes)
		pkgPath := strings.Trim(imp.Path.Value, "\"")

		// Get position
		pos := result.Fset.Position(imp.Pos())

		// Get alias name if present
		var name string
		if imp.Name != nil {
			name = imp.Name.Name
		}

		imports = append(imports, Import{
			FilePath:    filePath,
			PkgPath:     pkgPath,
			Name:        name,
			Line:        pos.Line,
			Col:         pos.Column,
			ImporterPkg: pkg.PkgPath,
		})
	}

	return imports
}
