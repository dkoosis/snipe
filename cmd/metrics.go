package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	metricsTopN  int
	metricsKind  string
	metricsGraph string
)

var metricsCmd = &cobra.Command{
	Use:     "metrics",
	Short:   "Show graph metrics (PageRank, etc.) over the import graph",
	GroupID: "advanced",
	Long: `Print graph metrics computed during indexing.

Defaults to the top-20 packages by import-graph PageRank.
Other metrics (betweenness, HITS, cycles) land in upcoming releases.

Examples:
  snipe metrics
  snipe metrics --top=10
  snipe metrics --kind=pagerank
  snipe metrics --format=json`,
	Args: cobra.NoArgs,
	RunE: runMetrics,
}

func init() {
	metricsCmd.Flags().IntVar(&metricsTopN, "top", 20, "Top-N rows to print")
	metricsCmd.Flags().StringVar(&metricsKind, "kind", "pagerank", "Metric kind (only 'pagerank' currently)")
	metricsCmd.Flags().StringVar(&metricsGraph, "graph", "imports", "Graph kind (only 'imports' currently)")
	rootCmd.AddCommand(metricsCmd)
}

func runMetrics(_ *cobra.Command, _ []string) error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Validate --kind. Empty results from ReadTopN signal "not yet populated".
	switch metricsKind {
	case "pagerank", "betweenness", "hits", "hub", "authority",
		"cycles", "topo", "degree", "in_degree", "out_degree", "eigenvector":
		// ok
	default:
		return w.WriteError("metrics", &output.Error{
			Code:    output.ErrInternal,
			Message: fmt.Sprintf("unknown --kind %q", metricsKind),
		})
	}

	s, dir, err := OpenStore(w, "metrics")
	if err != nil {
		return err
	}
	defer s.Close()

	// Topo sort is transient — recomputed on demand from the imports graph.
	if metricsKind == "topo" {
		return runTopoMetrics(s, dir, start)
	}

	rows, err := s.ReadTopN(metricsGraph, metricsKind, metricsTopN)
	if err != nil {
		return w.WriteError("metrics", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}

	if GetOutputFormat() == output.OutputJSON {
		return writeMetricsJSON(rows, dir, start)
	}

	return writeMetricsText(rows)
}

// metricRow is a JSON-friendly view of a graph_metrics row.
type metricRow struct {
	Rank  int     `json:"rank"`
	Node  string  `json:"node"`
	Value float64 `json:"value"`
}

func writeMetricsJSON(rows []store.MetricRow, dir string, start time.Time) error {
	out := make([]metricRow, len(rows))
	for i, r := range rows {
		out[i] = metricRow{Rank: r.Rank, Node: r.NodeID, Value: r.Value}
	}
	resp := output.Response[metricRow]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  out,
		Meta: output.Meta{
			Command:  "metrics",
			Query:    map[string]string{"graph": metricsGraph, "kind": metricsKind, "top": fmt.Sprintf("%d", metricsTopN)},
			RepoRoot: dir,
			Ms:       time.Since(start).Milliseconds(),
			Total:    len(out),
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

// topoResult is a JSON-friendly view of a topo-sort result.
type topoResult struct {
	Order  []string `json:"order,omitempty"`
	Cycle  []string `json:"cycle,omitempty"`
	Cyclic bool     `json:"cyclic"`
}

func runTopoMetrics(s *store.Store, dir string, start time.Time) error {
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	order, cycle := graphmetrics.TopoSort(g)

	if GetOutputFormat() == output.OutputJSON {
		res := topoResult{Order: order, Cycle: cycle, Cyclic: order == nil}
		resp := output.Response[topoResult]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []topoResult{res},
			Meta: output.Meta{
				Command:  "metrics",
				Query:    map[string]string{"graph": metricsGraph, "kind": "topo"},
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    len(order),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	if order == nil {
		fmt.Fprintf(&b, "%s graph · topo · cycle detected (%d nodes)\n", metricsGraph, len(cycle))
		for i, n := range cycle {
			fmt.Fprintf(&b, "  %2d  %s\n", i+1, n)
		}
	} else {
		fmt.Fprintf(&b, "%s graph · topo · %d nodes\n", metricsGraph, len(order))
		for i, n := range order {
			fmt.Fprintf(&b, "  %2d  %s\n", i+1, n)
		}
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

func writeMetricsText(rows []store.MetricRow) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s graph · %s · top %d\n", metricsGraph, metricsKind, len(rows))
	if len(rows) == 0 {
		b.WriteString("  (no metric rows — run `snipe index` to populate)\n")
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %2d  %.3f  %s\n", r.Rank, r.Value, r.NodeID)
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
