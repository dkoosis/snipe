package cmd

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// looksLikePackagePath returns true when arg plausibly names a package rather
// than a symbol or hex ID. Checks in order:
//  1. starts with "./" or "../"
//  2. contains "/" (Go symbols never do; hex IDs never do)
//  3. directory exists under the repo root and contains .go files
func looksLikePackagePath(repoRoot, arg string) bool {
	if strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") {
		return true
	}
	if strings.Contains(arg, "/") {
		return true
	}
	// Fall back: does the directory exist relative to repoRoot and contain .go files?
	if repoRoot == "" {
		return false
	}
	dir := filepath.Join(repoRoot, arg)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// runPackPackage builds a package-level pack response. `arg` is the raw user
// input (e.g. "internal/mcp", "./internal/mcp", "store").
func runPackPackage(w *output.Writer, s *store.Store, dir, arg string, start time.Time) error {
	db := s.DB()
	repoRoot, _ := s.GetMeta("repo_root")
	if repoRoot == "" {
		repoRoot = dir
	}

	// Normalize "./x" -> "x"
	cleanArg := strings.TrimPrefix(arg, "./")
	cleanArg = strings.TrimSuffix(cleanArg, "/...")

	pkgPattern := query.ResolvePkgPattern(db, cleanArg, dir, repoRoot)
	modulePath := query.DetectModulePath(db)
	fullPkgPath := query.ResolveFullPkgPath(db, pkgPattern, modulePath)

	// Verify the package exists in the index (have symbols OR is importer in imports).
	var exists int
	_ = db.QueryRow(`SELECT 1 FROM symbols WHERE pkg_path = ? LIMIT 1`, fullPkgPath).Scan(&exists)
	if exists == 0 {
		_ = db.QueryRow(`SELECT 1 FROM imports WHERE importer_pkg = ? LIMIT 1`, fullPkgPath).Scan(&exists)
	}
	if exists == 0 {
		return w.WriteError("pack", &output.Error{
			Code:    output.ErrNotFound,
			Message: "no package found matching: " + arg,
		})
	}

	// Exports via existing machinery.
	symbols, err := query.FindPackageSymbols(db, fullPkgPath, 500, 0)
	if err != nil {
		return w.WriteError("pack", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	exports := make([]output.PackageExport, 0, len(symbols))
	var keyTypes []string
	for _, sym := range symbols {
		exports = append(exports, output.PackageExport{
			ID:        sym.ID,
			Name:      sym.Name,
			Kind:      sym.Kind,
			Signature: sym.Signature.String,
			Line:      sym.LineStart,
		})
		if isTypeKind(sym.Kind) {
			keyTypes = append(keyTypes, sym.Name)
		}
	}
	if len(keyTypes) > 10 {
		keyTypes = keyTypes[:10]
	}

	// Imports (deps) + dependent count.
	var imports []output.DepRef
	dependentCount := 0
	if modulePath != "" {
		pd, derr := query.FindPackageDeps(db, fullPkgPath, modulePath)
		if derr == nil && pd != nil {
			imports = pd.Dependencies
			dependentCount = len(pd.Dependents)
		}
	}

	// Package directory + file-level stats (LOC, files, tests).
	pkgDir := pkgDirFromSymbols(db, fullPkgPath)
	fileCount, loc, testCount := computePackageStats(db, fullPkgPath, pkgDir)

	// Exported-symbol count (visible in index); denormalize since FindPackageSymbols
	// already filters to exports.
	exportCount := len(exports)

	displayPkg := fullPkgPath
	if modulePath != "" {
		if trim := strings.TrimPrefix(fullPkgPath, modulePath); trim != fullPkgPath {
			displayPkg = strings.TrimPrefix(trim, "/")
			if displayPkg == "" {
				displayPkg = "."
			}
		}
	}

	pkgDirRel := pkgDir
	if pkgDir != "" && repoRoot != "" {
		if rel, rErr := filepath.Rel(repoRoot, pkgDir); rErr == nil {
			pkgDirRel = rel
		}
	}

	result := output.PackPackageResult{
		Package:        displayPkg,
		ModulePath:     modulePath,
		Dir:            pkgDirRel,
		FileCount:      fileCount,
		LOC:            loc,
		TestCount:      testCount,
		ExportCount:    exportCount,
		Exports:        exports,
		Imports:        imports,
		DependentCount: dependentCount,
		KeyTypes:       keyTypes,
	}

	resp := output.Response[output.PackPackageResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.PackPackageResult{result},
		Meta: output.Meta{
			Command:    "pack",
			Query:      map[string]string{"package": displayPkg},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(db, dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      1,
		},
	}

	return w.WriteResponse(resp)
}

// pkgDirFromSymbols picks a representative .go file for the package and returns
// its directory (absolute). Empty string if the index has no file for it.
func pkgDirFromSymbols(db *sql.DB, pkgPath string) string {
	var filePath string
	err := db.QueryRow(`
		SELECT file_path FROM symbols WHERE pkg_path = ? LIMIT 1
	`, pkgPath).Scan(&filePath)
	if err != nil || filePath == "" {
		return ""
	}
	return filepath.Dir(filePath)
}

// computePackageStats returns (fileCount, loc, testCount) for the given package.
// LOC is counted from the filesystem (the index does not store a whole-file LOC).
// testCount is the number of Test* funcs in *_test.go files in the index.
func computePackageStats(db *sql.DB, pkgPath, pkgDir string) (int, int, int) {
	fileCount, loc := 0, 0
	if pkgDir != "" {
		entries, err := os.ReadDir(pkgDir)
		if err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
					continue
				}
				fileCount++
				data, rerr := os.ReadFile(filepath.Join(pkgDir, e.Name()))
				if rerr != nil {
					continue
				}
				loc += bytes.Count(data, []byte{'\n'})
				if len(data) > 0 && data[len(data)-1] != '\n' {
					loc++
				}
			}
		}
	}

	testCount := 0
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM symbols
		WHERE pkg_path = ?
		  AND kind = 'func'
		  AND name LIKE 'Test%'
		  AND file_path LIKE '%_test.go'
	`, pkgPath).Scan(&testCount)

	return fileCount, loc, testCount
}
