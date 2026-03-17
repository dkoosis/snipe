package index

import (
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
func ExtractImportsFiltered(result *LoadResult, onlyFiles map[string]bool) ([]Import, error) {
	var imports []Import

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

			fileImports := extractFileImports(pkg, file, filePath, result)
			imports = append(imports, fileImports...)
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
