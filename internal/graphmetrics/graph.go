// Package graphmetrics computes graph algorithms over snipe's import and call graphs.
package graphmetrics

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/store"
)

// Graph is a directed adjacency-list graph keyed by string node IDs.
type Graph struct {
	Adj map[string][]string // node -> outgoing edges (may be nil/empty)
}

// Nodes returns all node IDs (including those with no outgoing edges) sorted.
func (g *Graph) Nodes() []string {
	seen := make(map[string]struct{}, len(g.Adj))
	for n, outs := range g.Adj {
		seen[n] = struct{}{}
		for _, m := range outs {
			seen[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// OutEdges returns outgoing edges from n (nil if none).
func (g *Graph) OutEdges(n string) []string {
	return g.Adj[n]
}

// InEdges returns incoming edges to n (computed; not cached).
func (g *Graph) InEdges(n string) []string {
	var in []string
	srcs := make([]string, 0, len(g.Adj))
	for s := range g.Adj {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	for _, s := range srcs {
		for _, dst := range g.Adj[s] {
			if dst == n {
				in = append(in, s)
				break
			}
		}
	}
	return in
}

// LoadImportsGraph builds an intra-repo package import graph from the imports table.
// Only edges where both importer_pkg and pkg_path are inside the current module
// (per go.mod at repo_root) are included. Includes imports from _test.go files.
func LoadImportsGraph(s *store.Store) (*Graph, error) {
	return LoadImportsGraphOpts(s, true)
}

// LoadImportsGraphOpts is like LoadImportsGraph but lets the caller exclude
// edges that originate in _test.go files (test-only imports). Used by Ca/Ce
// computation to separate production coupling from test coupling.
func LoadImportsGraphOpts(s *store.Store, includeTests bool) (*Graph, error) {
	modPath, err := moduleImportPath(s)
	if err != nil {
		return nil, err
	}
	if modPath == "" {
		return nil, fmt.Errorf("could not determine module path for intra-repo filter")
	}
	prefix := modPath + "/"

	q := `
		SELECT DISTINCT importer_pkg, pkg_path
		FROM imports
		WHERE importer_pkg IS NOT NULL
		  AND importer_pkg <> ''
		  AND (importer_pkg = ? OR importer_pkg LIKE ?)
		  AND (pkg_path = ?      OR pkg_path     LIKE ?)
	`
	if !includeTests {
		// _test.go covers in-package test imports; ".test" suffix covers the
		// synthetic test-binary packages go/packages emits (e.g. "pkg.test").
		q += ` AND file_path NOT LIKE '%\_test.go' ESCAPE '\'`
		q += ` AND importer_pkg NOT LIKE '%.test'`
		q += ` AND pkg_path NOT LIKE '%.test'`
	}
	rows, err := s.DB().Query(q, modPath, prefix+"%", modPath, prefix+"%")
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	adj := make(map[string][]string)
	seenEdge := make(map[[2]string]struct{})
	for rows.Next() {
		var src, dst string
		if err := rows.Scan(&src, &dst); err != nil {
			return nil, fmt.Errorf("scan import row: %w", err)
		}
		if src == dst {
			continue
		}
		key := [2]string{src, dst}
		if _, ok := seenEdge[key]; ok {
			continue
		}
		seenEdge[key] = struct{}{}
		adj[src] = append(adj[src], dst)
		if _, ok := adj[dst]; !ok {
			adj[dst] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import rows: %w", err)
	}

	for k := range adj {
		if len(adj[k]) > 1 {
			sort.Strings(adj[k])
		}
	}
	return &Graph{Adj: adj}, nil
}

// LoadCallsGraph builds a directed graph from snipe's call_graph table.
// Nodes are symbol hex IDs; edges are caller -> callee.
// All callers/callees are in-repo by construction (call_graph only stores resolved edges),
// so no module-path filtering is needed. Includes edges originating in _test.go files.
func LoadCallsGraph(s *store.Store) (*Graph, error) {
	return LoadCallsGraphOpts(s, true)
}

// LoadCallsGraphOpts is like LoadCallsGraph but lets the caller exclude edges
// where either endpoint's symbol lives in a _test.go file. This keeps test
// helpers (e.g. testutil.NewStore, setupTestEnv) from dominating call-graph
// centrality metrics like PageRank.
func LoadCallsGraphOpts(s *store.Store, includeTests bool) (*Graph, error) {
	q := `
		SELECT DISTINCT caller_id, callee_id
		FROM call_graph
		WHERE caller_id IS NOT NULL AND callee_id IS NOT NULL
	`
	if !includeTests {
		q += `
			AND caller_id IN (SELECT id FROM symbols WHERE file_path NOT LIKE '%\_test.go' ESCAPE '\')
			AND callee_id IN (SELECT id FROM symbols WHERE file_path NOT LIKE '%\_test.go' ESCAPE '\')
		`
	}
	rows, err := s.DB().Query(q)
	if err != nil {
		return nil, fmt.Errorf("query call_graph: %w", err)
	}
	defer func() { _ = rows.Close() }()

	adj := make(map[string][]string)
	seenEdge := make(map[[2]string]struct{})
	for rows.Next() {
		var src, dst string
		if err := rows.Scan(&src, &dst); err != nil {
			return nil, fmt.Errorf("scan call_graph row: %w", err)
		}
		if src == dst {
			continue
		}
		key := [2]string{src, dst}
		if _, ok := seenEdge[key]; ok {
			continue
		}
		seenEdge[key] = struct{}{}
		adj[src] = append(adj[src], dst)
		if _, ok := adj[dst]; !ok {
			adj[dst] = nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate call_graph rows: %w", err)
	}

	for k := range adj {
		if len(adj[k]) > 1 {
			sort.Strings(adj[k])
		}
	}
	return &Graph{Adj: adj}, nil
}

// moduleImportPath reads the module path from go.mod at repo_root.
func moduleImportPath(s *store.Store) (string, error) {
	repoRoot, err := s.GetMeta("repo_root")
	if err != nil || repoRoot == "" {
		return "", fmt.Errorf("repo_root meta not set: %w", err)
	}
	goModPath := filepath.Join(repoRoot, "go.mod")
	f, err := os.Open(goModPath) // #nosec G304 -- repo_root comes from indexer
	if err != nil {
		return "", fmt.Errorf("open go.mod: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "module"))
			rest = strings.Trim(rest, "\"")
			return rest, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan go.mod: %w", err)
	}
	return "", fmt.Errorf("module directive not found in %s", goModPath)
}
