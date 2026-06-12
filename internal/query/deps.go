package query

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/dkoosis/snipe/internal/output"
)

// PackageDeps holds bidirectional dependencies for a single package.
type PackageDeps struct {
	Dependencies []output.DepRef
	Dependents   []output.DepRef
}

// DepGraphEdge represents a directed edge in the full dependency graph.
type DepGraphEdge struct {
	From      string
	To        string
	FileCount int
}

// DepGraph holds the full dependency graph for a module.
type DepGraph struct {
	Packages []string
	Edges    []DepGraphEdge
	Cycles   [][]string
}

// DetectModulePath finds the Go module path for the indexed repo.
// Order: meta module_path → go.mod at repo_root → shortest pkg_path heuristic.
// The heuristic alone fails on repos where every package lives under
// internal/ or cmd/ (no root package), so go.mod is authoritative.
func DetectModulePath(db *sql.DB) string {
	var metaPath string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'module_path'`).Scan(&metaPath); err == nil && metaPath != "" {
		return metaPath
	}

	var repoRoot string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'repo_root'`).Scan(&repoRoot); err == nil && repoRoot != "" {
		if data, err := os.ReadFile(filepath.Join(repoRoot, "go.mod")); err == nil {
			if mp := modfile.ModulePath(data); mp != "" {
				return mp
			}
		}
	}

	var pkgPath string
	err := db.QueryRow(`
		SELECT pkg_path FROM symbols
		WHERE pkg_path NOT LIKE '%/internal/%'
		  AND pkg_path NOT LIKE '%/cmd/%'
		ORDER BY LENGTH(pkg_path)
		LIMIT 1
	`).Scan(&pkgPath)
	if err != nil {
		return ""
	}
	return pkgPath
}

// ResolveFullPkgPath ensures pkgPath is a full package path as stored in the imports table.
// If pkgPath already exists as an importer_pkg or pkg_path, returns it unchanged.
// Otherwise tries modulePath + "/" + pkgPath.
func ResolveFullPkgPath(db *sql.DB, pkgPath, modulePath string) string {
	var exists int
	_ = db.QueryRow(`SELECT 1 FROM imports WHERE importer_pkg = ? OR pkg_path = ? LIMIT 1`,
		pkgPath, pkgPath).Scan(&exists)
	if exists == 1 {
		return pkgPath
	}
	candidate := modulePath + "/" + pkgPath
	_ = db.QueryRow(`SELECT 1 FROM imports WHERE importer_pkg = ? OR pkg_path = ? LIMIT 1`,
		candidate, candidate).Scan(&exists)
	if exists == 1 {
		return candidate
	}
	return pkgPath
}

// FindPackageDeps returns bidirectional dependencies for a single package.
// Only internal packages (those sharing the module path prefix) are included.
func FindPackageDeps(db *sql.DB, pkgPath, modulePath string) (*PackageDeps, error) {
	// What this package imports (internal only)
	deps, err := scanDepEdges(db, `
		SELECT pkg_path, COUNT(DISTINCT file_path) as file_count
		FROM imports
		WHERE importer_pkg = ? AND pkg_path LIKE ? || '/%'
		GROUP BY pkg_path
		ORDER BY file_count DESC
	`, modulePath, pkgPath, modulePath)
	if err != nil {
		return nil, fmt.Errorf("query dependencies for %s: %w", pkgPath, err)
	}

	// Who imports this package (internal only)
	dependents, err := scanDepEdges(db, `
		SELECT importer_pkg, COUNT(DISTINCT file_path) as file_count
		FROM imports
		WHERE pkg_path = ? AND importer_pkg LIKE ? || '/%'
		GROUP BY importer_pkg
		ORDER BY file_count DESC
	`, modulePath, pkgPath, modulePath)
	if err != nil {
		return nil, fmt.Errorf("query dependents for %s: %w", pkgPath, err)
	}

	return &PackageDeps{Dependencies: deps, Dependents: dependents}, nil
}

// scanDepEdges runs a query returning (pkg_path, file_count) rows and trims the module prefix.
func scanDepEdges(db *sql.DB, query, modulePath string, args ...any) ([]output.DepRef, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []output.DepRef
	for rows.Next() {
		var fullPath string
		var e output.DepRef
		if err := rows.Scan(&fullPath, &e.FileCount); err != nil {
			return nil, err
		}
		e.Package = trimModulePath(fullPath, modulePath)
		result = append(result, e)
	}
	return result, rows.Err()
}

// FindDepGraph builds the full internal dependency graph for a module.
func FindDepGraph(db *sql.DB, modulePath string) (*DepGraph, error) {
	rows, err := db.Query(`
		SELECT importer_pkg, pkg_path, COUNT(DISTINCT file_path) as file_count
		FROM imports
		WHERE importer_pkg LIKE ? || '%' AND pkg_path LIKE ? || '/%'
		  AND importer_pkg != pkg_path
		GROUP BY importer_pkg, pkg_path
		ORDER BY file_count DESC
	`, modulePath, modulePath)
	if err != nil {
		return nil, fmt.Errorf("query dep graph: %w", err)
	}
	defer rows.Close()

	pkgSet := make(map[string]struct{})
	var edges []DepGraphEdge

	for rows.Next() {
		var fromFull, toFull string
		var fileCount int
		if err := rows.Scan(&fromFull, &toFull, &fileCount); err != nil {
			return nil, fmt.Errorf("scan dep graph edge: %w", err)
		}
		from := trimModulePath(fromFull, modulePath)
		to := trimModulePath(toFull, modulePath)
		pkgSet[from] = struct{}{}
		pkgSet[to] = struct{}{}
		edges = append(edges, DepGraphEdge{From: from, To: to, FileCount: fileCount})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dep graph rows: %w", err)
	}

	packages := make([]string, 0, len(pkgSet))
	for pkg := range pkgSet {
		packages = append(packages, pkg)
	}

	cycles := detectCycles(edges)

	return &DepGraph{
		Packages: packages,
		Edges:    edges,
		Cycles:   cycles,
	}, nil
}

// detectCycles uses DFS three-color marking to find cycles in the dependency graph.
// Uses a path stack for correct cycle reconstruction across DFS restarts.
// Returns at most 10 cycles.
func detectCycles(edges []DepGraphEdge) [][]string {
	adj := make(map[string][]string)
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
	}

	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully processed
	)

	color := make(map[string]int)
	var path []string // current DFS path (stack)
	var cycles [][]string

	var dfs func(node string)
	dfs = func(node string) {
		if len(cycles) >= 10 {
			return
		}
		color[node] = gray
		path = append(path, node)

		for _, next := range adj[node] {
			if len(cycles) >= 10 {
				return
			}
			switch color[next] {
			case gray:
				// Found a cycle — extract from path stack
				// Find where `next` appears in the current path
				var cycle []string
				for i := len(path) - 1; i >= 0; i-- {
					cycle = append(cycle, path[i])
					if path[i] == next {
						break
					}
				}
				// Reverse to get forward order
				for i, j := 0, len(cycle)-1; i < j; i, j = i+1, j-1 {
					cycle[i], cycle[j] = cycle[j], cycle[i]
				}
				cycles = append(cycles, cycle)
			case white:
				dfs(next)
			}
		}

		path = path[:len(path)-1]
		color[node] = black
	}

	for node := range adj {
		if color[node] == white {
			dfs(node)
		}
	}

	return cycles
}

// trimModulePath strips the module path prefix from a full package path.
func trimModulePath(fullPath, modulePath string) string {
	rel := strings.TrimPrefix(fullPath, modulePath)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "."
	}
	return rel
}
