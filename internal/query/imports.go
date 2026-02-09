package query

import (
	"database/sql"
	"path/filepath"
	"strings"
)

// ImportRow represents an import record from the database
type ImportRow struct {
	FilePath    string
	PkgPath     string
	Name        sql.NullString
	Line        int
	Col         int
	ImporterPkg sql.NullString
}

// FindImports returns all imports for a given file
func FindImports(db *sql.DB, filePath string, limit, offset int) ([]ImportRow, error) {
	rows, err := db.Query(`
		SELECT file_path, pkg_path, name, line, col, importer_pkg
		FROM imports
		WHERE file_path = ?
		ORDER BY line
		LIMIT ? OFFSET ?
	`, filePath, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanImportRows(rows)
}

// FindImportsByPackage returns all imports of a given package path
// This is used for the "importers" command - who imports this package
func FindImportsByPackage(db *sql.DB, pkgPath string, limit, offset int) ([]ImportRow, error) {
	rows, err := db.Query(`
		SELECT file_path, pkg_path, name, line, col, importer_pkg
		FROM imports
		WHERE pkg_path = ? OR pkg_path LIKE ?
		ORDER BY file_path, line
		LIMIT ? OFFSET ?
	`, pkgPath, "%/"+pkgPath, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanImportRows(rows)
}

// FindImportersByDirectory returns files that import any package within a directory
// For "snipe importers internal/handler" - find who imports anything in that dir
func FindImportersByDirectory(db *sql.DB, dirPath string, limit, offset int) ([]ImportRow, error) {
	// Boundary-aware matching: match dirPath as a complete path segment
	// "internal/handler" matches ".../internal/handler" and ".../internal/handler/sub"
	// but NOT ".../internal/handler2"
	dirPath = strings.TrimPrefix(dirPath, "/")
	suffixMatch := "%/" + dirPath         // ends with /dirPath (exact package)
	subpkgSuffix := "%/" + dirPath + "/%" // subpackages: .../dirPath/...
	subpkgExact := dirPath + "/%"         // root-relative subpackages

	rows, err := db.Query(`
		SELECT file_path, pkg_path, name, line, col, importer_pkg
		FROM imports
		WHERE pkg_path = ? OR pkg_path LIKE ?
		   OR pkg_path LIKE ? OR pkg_path LIKE ?
		ORDER BY file_path, line
		LIMIT ? OFFSET ?
	`, dirPath, suffixMatch, subpkgSuffix, subpkgExact, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanImportRows(rows)
}

// FindImportsForDirectory returns all imports for files in a directory
func FindImportsForDirectory(db *sql.DB, dirPath string, limit, offset int) ([]ImportRow, error) {
	pattern := dirPath + "%"

	rows, err := db.Query(`
		SELECT file_path, pkg_path, name, line, col, importer_pkg
		FROM imports
		WHERE file_path LIKE ?
		ORDER BY file_path, line
		LIMIT ? OFFSET ?
	`, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanImportRows(rows)
}

// CountImportsByPackage returns count of files importing a package
func CountImportsByPackage(db *sql.DB, pkgPath string) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT file_path) FROM imports
		WHERE pkg_path = ? OR pkg_path LIKE ?
	`, pkgPath, "%/"+pkgPath).Scan(&count)
	return count, err
}

func scanImportRows(rows *sql.Rows) ([]ImportRow, error) {
	var imports []ImportRow
	for rows.Next() {
		var imp ImportRow
		err := rows.Scan(&imp.FilePath, &imp.PkgPath, &imp.Name, &imp.Line, &imp.Col, &imp.ImporterPkg)
		if err != nil {
			return nil, err
		}
		imports = append(imports, imp)
	}
	return imports, rows.Err()
}

// ResolveFileOrPackage determines if input is a file path or package path
// Returns (isFile, normalizedPath)
func ResolveFileOrPackage(input, repoRoot string) (bool, string) {
	// If it ends with .go, it's a file
	if strings.HasSuffix(input, ".go") {
		// Make absolute if relative
		if !filepath.IsAbs(input) {
			input = filepath.Join(repoRoot, input)
		}
		return true, input
	}

	// If it contains path separators, treat as directory pattern
	if strings.Contains(input, "/") || strings.Contains(input, string(filepath.Separator)) {
		return false, input // Treat as package/directory pattern
	}

	// Otherwise treat as package name
	return false, input
}
