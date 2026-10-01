package graphmetrics

import (
	"fmt"
	"io"
	"time"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

// Compute runs every index-time metric over the freshly written index and
// persists the results: import-graph and call-graph centrality, coupling,
// cohesion, complexity rollups, and the file-level risk metrics (complexity,
// fan-in, git churn). Each step is best-effort — a failure is reported on log
// and the remaining steps still run. It then stamps the index with the current
// file-metrics generation so NeedBackfill can detect a stale one.
func Compute(s *store.Store, symbols []index.Symbol, log io.Writer) {
	steps := []struct {
		label string
		run   func() error
	}{
		{"SCC", func() error { return computeImportSCCs(s, log) }},
		{"metrics", func() error { return computeImportPageRank(s, log) }},
		{"HITS", func() error { return computeImportHITS(s, log) }},
		{"degree/eigenvector", func() error { return computeImportDegreeAndEigenvector(s, log) }},
		{"coupling", func() error { return computeImportCoupling(s, log) }},
		{"abstractness", func() error { return computeAbstractness(s, log) }},
		{"LCOM4", func() error { return computeLCOM4(s, log) }},
		{"cyclo rollup", func() error { return computeCycloRollups(s, log, symbols) }},
		{"cognitive rollup", func() error { return computeCognitiveRollups(s, log, symbols) }},
		{"calls-graph metrics", func() error { return computeCallsGraphMetrics(s, log) }},
		{"file complexity", func() error { return computeFileComplexity(s, symbols) }},
		{"file fan-in", func() error { return computeFileFanIn(s) }},
		{"git churn", func() error { return computeChurn(s, log) }},
	}
	for _, st := range steps {
		if err := st.run(); err != nil {
			fmt.Fprintf(log, "Warning: %s computation failed: %v\n", st.label, err)
		}
	}
	// Mark which generation of file-level metrics this index carries, so a
	// later skip/incremental run can detect a stale index and backfill.
	if err := s.SetMeta(metaFileMetricsVersion, fileMetricsVersion); err != nil {
		fmt.Fprintf(log, "Warning: failed to write file metrics version: %v\n", err)
	}
}

// metaFileMetricsVersion names the meta key tracking the generation of
// file-level risk metrics (file_churn + the "files" complexity/fan-in graph)
// an index carries. fileMetricsVersion is the current generation — bump it
// whenever a new file-level metric is added so existing indexes auto-backfill
// on their next `snipe index`. Repurposed (v2, sn-hmz) as the force-reindex
// lever for the go-stmt/channel-op ast_ctx signal too: any version bump here
// forces a one-time full reindex, which is what's needed to backfill refs
// for an ast_ctx value added after the index was built (schemaVersion is
// unaffected — the index fingerprint excludes it, so bumping schemaVersion
// alone would NOT trigger a reindex).
//
// v3 (sn-8f6q.9): inline fixture-path literals are a new string_refs extraction
// shape. Without a bump, an index built before this change keeps the old rows —
// change detection compares only the dependency fingerprint and unchanged
// source, so it skips, and `plan` reports the index fresh while missing WILL
// CHURN paths until each affected test happens to change (or --force). The bump
// forces the one-time full reindex that backfills them.
const (
	metaFileMetricsVersion = "file_metrics_version"
	fileMetricsVersion     = "3"
)

// NeedBackfill reports whether the index lacks the current generation of
// file-level metrics — true for any index built before the marker was written
// (or before a metric was added). It is the signal to force a full reindex
// even when change detection would otherwise skip or go incremental.
func NeedBackfill(s *store.Store) bool {
	// A missing key returns ("", err); an unreadable DB returns ("", err) too.
	// Either way the value won't equal the current version, so we backfill —
	// a one-time full reindex is the safe outcome, never data loss.
	v, _ := s.GetMeta(metaFileMetricsVersion)
	return v != fileMetricsVersion
}

// computeImportSCCs loads the intra-repo imports graph, computes nontrivial
// strongly-connected components via Tarjan, and persists them into graph_sccs.
func computeImportSCCs(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	g, err := LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	sccs := SCC(g)
	if err := s.WriteSCCs("imports", sccs); err != nil {
		return fmt.Errorf("write graph sccs: %w", err)
	}
	fmt.Fprintf(log, "metrics: detected %d nontrivial SCCs in %dms\n",
		len(sccs), time.Since(t0).Milliseconds())
	return nil
}

// computeImportPageRank loads the intra-repo imports graph, runs PageRank,
// and persists results into graph_metrics. Best-effort: callers log+continue.
func computeImportPageRank(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	g, err := LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	scores := PageRank(g, 0.85, 50)
	if err := s.WriteGraphMetrics("imports", "pagerank", scores); err != nil {
		return fmt.Errorf("write graph metrics: %w", err)
	}
	fmt.Fprintf(log, "metrics: pagerank computed for %d packages in %dms\n",
		len(scores), time.Since(t0).Milliseconds())
	return nil
}

// computeImportHITS loads the intra-repo imports graph, runs Kleinberg HITS,
// and persists hub and authority vectors as separate graph_metrics rows.
func computeImportHITS(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	g, err := LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	hubs, auths := HITS(g, 50)
	if err := s.WriteGraphMetrics("imports", "hub", hubs); err != nil {
		return fmt.Errorf("write hub metrics: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "authority", auths); err != nil {
		return fmt.Errorf("write authority metrics: %w", err)
	}
	fmt.Fprintf(log, "metrics: HITS computed for %d packages in %dms\n",
		len(hubs), time.Since(t0).Milliseconds())
	return nil
}

// computeImportDegreeAndEigenvector loads the intra-repo imports graph, computes
// in-degree, out-degree, and eigenvector centrality, and persists each as its
// own metric row.
func computeImportDegreeAndEigenvector(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	g, err := LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	in := InDegree(g)
	out := OutDegree(g)
	eig := EigenvectorCentrality(g, 50)
	if err := s.WriteGraphMetrics("imports", "in_degree", in); err != nil {
		return fmt.Errorf("write in_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "out_degree", out); err != nil {
		return fmt.Errorf("write out_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "eigenvector", eig); err != nil {
		return fmt.Errorf("write eigenvector: %w", err)
	}
	fmt.Fprintf(log, "metrics: degree+eigenvector computed for %d packages in %dms\n",
		len(in), time.Since(t0).Milliseconds())
	return nil
}

// computeImportCoupling loads the intra-repo imports graph (production-only,
// excluding edges from _test.go files) and persists per-package Ca and Ce
// (afferent and efferent coupling) as separate metric kinds. Foundation for
// instability (I = Ce/(Ca+Ce)) and other Martin-style architecture metrics.
func computeImportCoupling(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	g, err := LoadImportsGraphOpts(s, false)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	coup := ComputeCoupling(g)
	ca := make(map[string]float64, len(coup))
	ce := make(map[string]float64, len(coup))
	in := make(map[string]float64, len(coup))
	for n, c := range coup {
		ca[n] = float64(c.Ca)
		ce[n] = float64(c.Ce)
		in[n] = c.I()
	}
	if err := s.WriteGraphMetrics("imports", "ca", ca); err != nil {
		return fmt.Errorf("write ca: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "ce", ce); err != nil {
		return fmt.Errorf("write ce: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "instability", in); err != nil {
		return fmt.Errorf("write instability: %w", err)
	}
	fmt.Fprintf(log, "metrics: Ca/Ce/I computed for %d packages in %dms\n",
		len(coup), time.Since(t0).Milliseconds())
	return nil
}

// computeCallsGraphMetrics loads the call graph and computes the same family of
// metrics as the imports graph: PageRank, HITS (hub+authority), in/out degree,
// eigenvector centrality, betweenness, and SCCs. Persisted with graph_kind="calls".
//
// Topo sort is intentionally skipped — recursion is normal in code, and a cycle
// witness on the call graph isn't useful output. SCC already surfaces recursion
// clusters in a more digestible form.
func computeCallsGraphMetrics(s *store.Store, log io.Writer) error {
	t0 := time.Now()
	// Exclude _test.go-rooted edges so test helpers (testutil.NewStore,
	// setupTestEnv) don't dominate centrality metrics. snipe-7fk.
	g, err := LoadCallsGraphOpts(s, false)
	if err != nil {
		return fmt.Errorf("load calls graph: %w", err)
	}

	// SCC — recursion clusters.
	sccs := SCC(g)
	if err := s.WriteSCCs("calls", sccs); err != nil {
		return fmt.Errorf("write calls sccs: %w", err)
	}

	// PageRank.
	pr := PageRank(g, 0.85, 50)
	if err := s.WriteGraphMetrics("calls", "pagerank", pr); err != nil {
		return fmt.Errorf("write calls pagerank: %w", err)
	}

	// HITS hub + authority.
	hubs, auths := HITS(g, 50)
	if err := s.WriteGraphMetrics("calls", "hub", hubs); err != nil {
		return fmt.Errorf("write calls hub: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "authority", auths); err != nil {
		return fmt.Errorf("write calls authority: %w", err)
	}

	// Degree + eigenvector.
	in := InDegree(g)
	out := OutDegree(g)
	eig := EigenvectorCentrality(g, 50)
	if err := s.WriteGraphMetrics("calls", "in_degree", in); err != nil {
		return fmt.Errorf("write calls in_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "out_degree", out); err != nil {
		return fmt.Errorf("write calls out_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "eigenvector", eig); err != nil {
		return fmt.Errorf("write calls eigenvector: %w", err)
	}

	// Brandes betweenness — O(V*E). Call graph is small (~600 nodes, ~2.7k edges
	// on snipe itself) so this stays well under a second.
	bc := Betweenness(g)
	if err := s.WriteGraphMetrics("calls", "betweenness", bc); err != nil {
		return fmt.Errorf("write calls betweenness: %w", err)
	}

	fmt.Fprintf(log, "metrics: calls graph computed for %d symbols in %dms\n",
		len(in), time.Since(t0).Milliseconds())
	return nil
}
