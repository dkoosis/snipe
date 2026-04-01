package context

import (
	"database/sql"
	"strings"
)

// getPackagePurposes returns all packages with their inferred purposes.
// Queries distinct pkg_paths and shortens them in Go (SQLite lacks REVERSE).
// Uses real doc comments from package_docs table when available, falling back
// to hardcoded inference.
func getPackagePurposes(db *sql.DB, repoRoot string) ([]PackagePurpose, error) {
	rows, err := db.Query(`
		SELECT DISTINCT pkg_path
		FROM symbols
		WHERE file_path LIKE ? || '/%'
		  AND pkg_path IS NOT NULL
		  AND pkg_path != ''
		ORDER BY pkg_path
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pkgPaths []string
	for rows.Next() {
		var pkgPath string
		if err := rows.Scan(&pkgPath); err != nil {
			continue
		}
		pkgPaths = append(pkgPaths, pkgPath)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Batch load package docs
	docMap := loadPackageDocs(db, pkgPaths)

	seen := make(map[string]bool)
	var components []PackagePurpose
	for _, pkgPath := range pkgPaths {
		name := shortenPackagePath(pkgPath)
		name = normalizePackageName(name)
		if name == "" || seen[name] {
			continue
		}
		// Skip _test pseudo-packages — they duplicate the real package
		if strings.HasSuffix(name, "_test") {
			continue
		}
		seen[name] = true

		purpose := docMap[pkgPath]
		if purpose == "" {
			purpose = inferPackagePurpose(name)
		}
		components = append(components, PackagePurpose{
			Name:    name,
			Purpose: purpose,
		})
	}

	return components, nil
}

// loadPackageDocs returns a map of pkg_path -> first sentence of doc comment.
// Non-fatal: returns empty map if package_docs table doesn't exist yet.
func loadPackageDocs(db *sql.DB, pkgPaths []string) map[string]string {
	if len(pkgPaths) == 0 {
		return nil
	}

	placeholders := make([]string, len(pkgPaths))
	args := make([]interface{}, len(pkgPaths))
	for i, p := range pkgPaths {
		placeholders[i] = "?"
		args[i] = p
	}

	// #nosec G201 -- placeholders are positional parameters
	query := "SELECT pkg_path, doc FROM package_docs WHERE pkg_path IN (" +
		strings.Join(placeholders, ",") + ")"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]string, len(pkgPaths))
	for rows.Next() {
		var pkgPath, doc string
		if err := rows.Scan(&pkgPath, &doc); err != nil {
			continue
		}
		purpose := tersePurpose(ExtractFirstSentence(doc))
		// Discard placeholder purposes that don't convey real information
		if !isPlaceholderPurpose(purpose) {
			result[pkgPath] = purpose
		}
	}
	return result
}

// tersePurpose strips godoc-style "Package X provides/handles/implements..." prefix
// to normalize all purposes to terse fragment voice.
func tersePurpose(s string) string {
	// Match "Package <name> <verb>s ..." — strip prefix through the verb
	if !strings.HasPrefix(s, "Package ") {
		return s
	}
	// Find the verb after "Package <name> "
	rest := strings.TrimPrefix(s, "Package ")
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx < 0 {
		return s
	}
	afterName := rest[spaceIdx+1:]
	// Common godoc verbs — strip and capitalize the remainder
	for _, verb := range []string{"provides ", "handles ", "implements ", "contains ", "defines ", "manages ", "offers ", "exposes "} {
		if strings.HasPrefix(afterName, verb) {
			remainder := afterName[len(verb):]
			if len(remainder) > 0 {
				// Capitalize first letter
				return strings.ToUpper(remainder[:1]) + remainder[1:]
			}
		}
	}
	// Also handle "is a ..." / "is the ..."
	for _, verb := range []string{"is a ", "is the ", "is "} {
		if strings.HasPrefix(afterName, verb) {
			remainder := afterName[len(verb):]
			if len(remainder) > 0 {
				return strings.ToUpper(remainder[:1]) + remainder[1:]
			}
		}
	}
	// Fall through: strip "Package <name> " prefix entirely, keep the rest as-is
	// Handles any pattern not matching known verbs above
	if len(afterName) > 0 {
		return strings.ToUpper(afterName[:1]) + afterName[1:]
	}
	return s
}

// isPlaceholderPurpose returns true if the purpose string is a generic placeholder
// that doesn't convey real architectural information.
func isPlaceholderPurpose(purpose string) bool {
	lower := strings.ToLower(purpose)
	placeholders := []string{
		"project root package",
		"application logic",
		"implementation",
		"package",
	}
	for _, p := range placeholders {
		if lower == p {
			return true
		}
	}
	return false
}

// normalizePackageName cleans up package names for display.
func normalizePackageName(name string) string {
	// Remove leading/trailing slashes
	name = strings.Trim(name, "/")

	// Skip empty or test-only packages
	if name == "" || name == "_test" {
		return ""
	}

	return name
}

// inferPackagePurpose determines a package's purpose from its name.
// This reuses the same logic as inferPurpose in generate.go but with full paths.
func inferPackagePurpose(pkg string) string {
	// Map of package patterns to purposes
	purposes := map[string]string{
		"cmd":              "CLI commands and entry points",
		"internal/store":   "SQLite persistence and database operations",
		"internal/query":   "Symbol lookup and reference queries",
		"internal/index":   "Go package loading and symbol extraction",
		"internal/output":  "JSON/human output formatting",
		"internal/config":  "Configuration management",
		"internal/search":  "Ripgrep integration and search",
		"internal/embed":   "Vector embeddings and similarity",
		"internal/context": "Boot context and LLM summaries",
		"internal/analyze": "Function analysis and diagnostics",
		"internal/edit":    "AST-safe code editing operations",
		"internal/kg":      "Knowledge graph integration (orca)",
		"internal/metrics": "Index and query metrics collection",
		"internal/util":    "Shared utility functions (project root, caching)",
		"internal/vector":  "Vector math for embedding similarity",
		"pkg":              "Public library packages",
		"api":              "API definitions and handlers",
		"test":             "Test utilities and fixtures",
		"bench":            "Benchmarks and baseline capture",
		"test/blackbox":    "Integration tests (blackbox)",
		"test/bench":       "Benchmarks and baseline capture",
	}

	// Check for exact match first
	if purpose, ok := purposes[pkg]; ok {
		return purpose
	}

	// Check for prefix matches
	for pattern, purpose := range purposes {
		if strings.HasPrefix(pkg, pattern) {
			return purpose
		}
	}

	// Leaf packages without a path separator and no doc comment — use last-segment heuristic
	// (Don't label everything "Project root package" — only the actual root gets that via doc)

	// Infer from last segment of package path
	parts := strings.Split(pkg, "/")
	lastPart := parts[len(parts)-1]

	segmentPurposes := map[string]string{
		"store":    "Data storage and persistence",
		"query":    "Query execution",
		"index":    "Indexing operations",
		"output":   "Output formatting",
		"config":   "Configuration",
		"search":   "Search functionality",
		"embed":    "Embeddings",
		"context":  "Context generation",
		"analyze":  "Analysis",
		"util":     "Utility functions",
		"utils":    "Utility functions",
		"internal": "Internal implementation",
	}

	if purpose, ok := segmentPurposes[lastPart]; ok {
		return purpose
	}

	return "Application logic"
}

// shortenPackagePath extracts the short form of a package path.
// Example: "github.com/user/snipe/internal/store" -> "internal/store"
func shortenPackagePath(pkgPath string) string {
	// Find /internal/ or /cmd/ and take from there
	if idx := strings.Index(pkgPath, "/internal/"); idx != -1 {
		return pkgPath[idx+1:]
	}
	if idx := strings.Index(pkgPath, "/cmd/"); idx != -1 {
		return pkgPath[idx+1:]
	}
	if strings.HasSuffix(pkgPath, "/cmd") {
		return "cmd"
	}

	// Root module package — use last segment (project name)
	parts := strings.Split(pkgPath, "/")
	return parts[len(parts)-1]
}
