package context

import (
	"database/sql"
	"sort"
	"strings"
)

// GenerateArchitectureSummary creates a high-level architecture overview from the snipe index.
// It analyzes call graph data to produce:
// - Spine: primary call flows from entry points
// - Components: packages with their inferred purposes
// - Edges: top cross-package call relationships
// - Description: placeholder for LLM-generated prose
//
// Performance: Uses batch SQL queries with no queries in loops. Total queries <= 5.
func GenerateArchitectureSummary(db *sql.DB, repoRoot string) (*ArchSummary, error) {
	// Query 1-3: Get primary flows (reuses batch queries from flows.go)
	spine, err := ExtractPrimaryFlows(db, repoRoot, 5)
	if err != nil {
		// Non-fatal: continue with empty spine
		spine = nil
	}

	// Query 4: Get package purposes in a single batch query
	components, err := getPackagePurposes(db, repoRoot)
	if err != nil {
		// Non-fatal: continue with empty components
		components = nil
	}

	// Query 5: Get cross-package call edges in a single batch query
	edges, err := getCrossPackageEdges(db, repoRoot)
	if err != nil {
		// Non-fatal: continue with empty edges
		edges = nil
	}

	// Build description placeholder
	description := "[Architecture description pending LLM enrichment]"

	return &ArchSummary{
		Spine:       spine,
		Components:  components,
		Edges:       edges,
		Description: description,
	}, nil
}

// getPackagePurposes returns all packages with their inferred purposes.
// Uses a single batch query to extract distinct packages from the index.
func getPackagePurposes(db *sql.DB, repoRoot string) ([]PackagePurpose, error) {
	// Query distinct package paths and group by top-level directory
	rows, err := db.Query(`
		SELECT DISTINCT
			CASE
				WHEN pkg_path LIKE '%/internal/%' THEN
					SUBSTR(pkg_path, INSTR(pkg_path, '/internal/') + 1,
						CASE
							WHEN INSTR(SUBSTR(pkg_path, INSTR(pkg_path, '/internal/') + 10), '/') > 0
							THEN INSTR(SUBSTR(pkg_path, INSTR(pkg_path, '/internal/') + 10), '/') + 8
							ELSE LENGTH(pkg_path)
						END
					)
				WHEN pkg_path LIKE '%/cmd/%' THEN 'cmd'
				WHEN pkg_path LIKE '%/cmd' THEN 'cmd'
				ELSE
					CASE
						WHEN INSTR(SUBSTR(pkg_path, 1), '/') > 0
						THEN SUBSTR(pkg_path, LENGTH(pkg_path) - INSTR(REVERSE(pkg_path), '/') + 2)
						ELSE pkg_path
					END
			END as pkg_short
		FROM symbols
		WHERE file_path LIKE ? || '/%'
		  AND pkg_path IS NOT NULL
		  AND pkg_path != ''
		GROUP BY pkg_short
		ORDER BY pkg_short
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect unique packages
	seen := make(map[string]bool)
	var components []PackagePurpose

	for rows.Next() {
		var pkgShort sql.NullString
		if err := rows.Scan(&pkgShort); err != nil {
			continue
		}
		if !pkgShort.Valid || pkgShort.String == "" {
			continue
		}

		name := normalizePackageName(pkgShort.String)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		// Infer purpose from package name
		purpose := inferPackagePurpose(name)
		components = append(components, PackagePurpose{
			Name:    name,
			Purpose: purpose,
		})
	}

	return components, rows.Err()
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
		"pkg":              "Public library packages",
		"api":              "API definitions and handlers",
		"test":             "Test utilities and fixtures",
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

// getCrossPackageEdges returns the top cross-package call relationships.
// Uses a single batch query with aggregation.
func getCrossPackageEdges(db *sql.DB, repoRoot string) ([]CrossPackageEdge, error) {
	// Query cross-package calls grouped by caller/callee package
	rows, err := db.Query(`
		WITH PackageCalls AS (
			SELECT
				caller.pkg_path as caller_pkg,
				callee.pkg_path as callee_pkg,
				COUNT(*) as call_count
			FROM call_graph cg
			JOIN symbols caller ON cg.caller_id = caller.id
			JOIN symbols callee ON cg.callee_id = callee.id
			WHERE caller.file_path LIKE ? || '/%'
			  AND callee.file_path LIKE ? || '/%'
			  AND caller.pkg_path IS NOT NULL
			  AND callee.pkg_path IS NOT NULL
			  AND caller.pkg_path != callee.pkg_path
			GROUP BY caller.pkg_path, callee.pkg_path
			HAVING call_count >= 2
		)
		SELECT caller_pkg, callee_pkg, call_count
		FROM PackageCalls
		ORDER BY call_count DESC
		LIMIT 15
	`, repoRoot, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []CrossPackageEdge
	for rows.Next() {
		var callerPkg, calleePkg string
		var count int
		if err := rows.Scan(&callerPkg, &calleePkg, &count); err != nil {
			continue
		}

		// Shorten package paths for display
		fromShort := shortenPackagePath(callerPkg)
		toShort := shortenPackagePath(calleePkg)

		edges = append(edges, CrossPackageEdge{
			From:  fromShort,
			To:    toShort,
			Count: count,
		})
	}

	return edges, rows.Err()
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

	// Take last two segments
	parts := strings.Split(pkgPath, "/")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	if len(parts) == 1 {
		return parts[0]
	}

	return pkgPath
}

// FormatArchSummary formats an ArchSummary as a human-readable string.
// Useful for debugging and display purposes.
func FormatArchSummary(summary *ArchSummary) string {
	if summary == nil {
		return "No architecture summary available"
	}

	var sb strings.Builder

	// Spine (primary flows)
	if len(summary.Spine) > 0 {
		sb.WriteString("Primary Flows:\n")
		for _, flow := range summary.Spine {
			sb.WriteString("  ")
			sb.WriteString(flow)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Components
	if len(summary.Components) > 0 {
		sb.WriteString("Components:\n")
		for _, comp := range summary.Components {
			sb.WriteString("  ")
			sb.WriteString(comp.Name)
			sb.WriteString(": ")
			sb.WriteString(comp.Purpose)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Edges (sorted by count for readability)
	if len(summary.Edges) > 0 {
		sb.WriteString("Cross-Package Calls:\n")
		// Sort by count descending
		sorted := make([]CrossPackageEdge, len(summary.Edges))
		copy(sorted, summary.Edges)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Count > sorted[j].Count
		})
		for _, edge := range sorted {
			sb.WriteString("  ")
			sb.WriteString(edge.From)
			sb.WriteString(" -> ")
			sb.WriteString(edge.To)
			sb.WriteString(" (")
			sb.WriteString(strings.Repeat("x", min(edge.Count/5, 10))) // Visual indicator
			if edge.Count > 50 {
				sb.WriteString("...")
			}
			sb.WriteString(" ")
			sb.WriteString(formatCount(edge.Count))
			sb.WriteString(")\n")
		}
		sb.WriteString("\n")
	}

	// Description
	if summary.Description != "" {
		sb.WriteString("Description:\n  ")
		sb.WriteString(summary.Description)
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatCount formats a count for display.
func formatCount(count int) string {
	if count == 1 {
		return "1 call"
	}
	return strings.Replace("N calls", "N", intToString(count), 1)
}

// intToString converts an integer to string without fmt import overhead.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + intToString(-n)
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
