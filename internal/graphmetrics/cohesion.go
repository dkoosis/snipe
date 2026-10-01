package graphmetrics

import (
	"fmt"
	"io"
	"time"

	"github.com/dkoosis/snipe/internal/store"
)

// computeAbstractness persists per-package A = abstract / total exported type
// decls (production files only). "Abstract" = interface decls; "total" =
// interface + struct + type. Pure-interface pkg → 1.0; pure-impl → 0.0;
// zero-type pkgs are skipped (no row written). Pairs with instability for
// distance-from-main-sequence (D = |A + I - 1|).
func computeAbstractness(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	rows, err := s.DB().Query(`
		SELECT pkg_path,
		       SUM(CASE WHEN kind = 'interface' THEN 1 ELSE 0 END) AS abstract,
		       SUM(CASE WHEN kind IN ('interface','struct','type') THEN 1 ELSE 0 END) AS total
		FROM symbols
		WHERE pkg_path IS NOT NULL AND pkg_path != ''
		  AND file_path NOT LIKE '%_test.go'
		  AND substr(name, 1, 1) GLOB '[A-Z]'
		GROUP BY pkg_path
		HAVING total > 0
	`)
	if err != nil {
		return fmt.Errorf("query abstractness: %w", err)
	}
	defer rows.Close()
	values := make(map[string]float64)
	for rows.Next() {
		var pkg string
		var abstract, total int
		if err := rows.Scan(&pkg, &abstract, &total); err != nil {
			return fmt.Errorf("scan abstractness: %w", err)
		}
		values[pkg] = float64(abstract) / float64(total)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iter abstractness: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "abstractness", values); err != nil {
		return fmt.Errorf("write abstractness: %w", err)
	}
	fmt.Fprintf(log, "metrics: abstractness computed for %d packages in %dms\n",
		len(values), time.Since(t0).Milliseconds())
	return nil
}

// computeLCOM4 persists per-package LCOM4 = number of connected components in
// the intra-package call graph (production funcs/methods, no _test.go). Nodes
// are top-level functions in a package; edges are intra-package func→func
// references (treated undirected). LCOM4 = 1 means the package is internally
// connected; LCOM4 > 1 surfaces split candidates (cross-reference with
// `snipe boundary`). Singleton packages (1 function) report LCOM4 = 1.
func computeLCOM4(s *store.Store, log io.Writer) error {
	t0 := time.Now()

	pkgNodes := make(map[string]map[string]struct{})
	nodeRows, err := s.DB().Query(`
		SELECT pkg_path, id FROM symbols
		WHERE kind IN ('func','method')
		  AND pkg_path IS NOT NULL AND pkg_path != ''
		  AND pkg_path NOT LIKE '%.test'
		  AND file_path NOT LIKE '%_test.go'
	`)
	if err != nil {
		return fmt.Errorf("query lcom4 nodes: %w", err)
	}
	for nodeRows.Next() {
		var pkg, id string
		if err := nodeRows.Scan(&pkg, &id); err != nil {
			nodeRows.Close()
			return fmt.Errorf("scan lcom4 node: %w", err)
		}
		set, ok := pkgNodes[pkg]
		if !ok {
			set = make(map[string]struct{})
			pkgNodes[pkg] = set
		}
		set[id] = struct{}{}
	}
	nodeRows.Close()
	if err := nodeRows.Err(); err != nil {
		return fmt.Errorf("iter lcom4 nodes: %w", err)
	}

	edgeRows, err := s.DB().Query(`
		SELECT s_enc.pkg_path, r.enclosing_id, r.symbol_id
		FROM refs r
		JOIN symbols s_enc ON s_enc.id = r.enclosing_id
		JOIN symbols s_tgt ON s_tgt.id = r.symbol_id
		WHERE r.enclosing_id IS NOT NULL
		  AND r.enclosing_id != r.symbol_id
		  AND s_enc.pkg_path = s_tgt.pkg_path
		  AND s_enc.pkg_path IS NOT NULL AND s_enc.pkg_path != ''
		  AND s_enc.pkg_path NOT LIKE '%.test'
		  AND s_enc.kind IN ('func','method')
		  AND s_tgt.kind IN ('func','method')
		  AND s_enc.file_path NOT LIKE '%_test.go'
		  AND s_tgt.file_path NOT LIKE '%_test.go'
	`)
	if err != nil {
		return fmt.Errorf("query lcom4 edges: %w", err)
	}
	pkgEdges := make(map[string][][2]string)
	for edgeRows.Next() {
		var pkg, src, dst string
		if err := edgeRows.Scan(&pkg, &src, &dst); err != nil {
			edgeRows.Close()
			return fmt.Errorf("scan lcom4 edge: %w", err)
		}
		pkgEdges[pkg] = append(pkgEdges[pkg], [2]string{src, dst})
	}
	edgeRows.Close()
	if err := edgeRows.Err(); err != nil {
		return fmt.Errorf("iter lcom4 edges: %w", err)
	}

	values := make(map[string]float64, len(pkgNodes))
	for pkg, nodes := range pkgNodes {
		values[pkg] = float64(componentCount(nodes, pkgEdges[pkg]))
	}

	if err := s.WriteGraphMetrics("imports", "lcom4", values); err != nil {
		return fmt.Errorf("write lcom4: %w", err)
	}
	fmt.Fprintf(log, "metrics: LCOM4 computed for %d packages in %dms\n",
		len(values), time.Since(t0).Milliseconds())
	return nil
}

// componentCount returns the number of connected components among nodes when
// edges are treated as undirected. An edge with an endpoint outside nodes is
// ignored.
func componentCount(nodes map[string]struct{}, edges [][2]string) int {
	parent := make(map[string]string, len(nodes))
	for n := range nodes {
		parent[n] = n
	}
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, e := range edges {
		if _, ok := nodes[e[0]]; !ok {
			continue
		}
		if _, ok := nodes[e[1]]; !ok {
			continue
		}
		if ra, rb := find(e[0]), find(e[1]); ra != rb {
			parent[ra] = rb
		}
	}
	roots := make(map[string]struct{}, len(nodes))
	for n := range nodes {
		roots[find(n)] = struct{}{}
	}
	return len(roots)
}
