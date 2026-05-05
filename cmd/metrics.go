package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	metricsPkg   string
)

var metricsCmd = &cobra.Command{
	Use:     "metrics",
	Short:   "Show graph metrics (PageRank, coupling, HITS, etc.) over the import or call graph",
	GroupID: "advanced",
	Long: `Print graph metrics computed during indexing.

Defaults to the top-20 packages by import-graph PageRank.
Use --graph=calls to rank symbols by call-graph metrics instead.

Examples:
  snipe metrics
  snipe metrics --top=10
  snipe metrics --kind=pagerank
  snipe metrics --graph=calls --kind=pagerank
  snipe metrics --graph=calls --kind=cycles
  snipe metrics --kind=coupling
  snipe metrics --kind=coupling --pkg=internal/store
  snipe metrics --format=json`,
	Args: cobra.NoArgs,
	RunE: runMetrics,
}

func init() {
	metricsCmd.Flags().IntVar(&metricsTopN, "top", 20, "Top-N rows to print")
	metricsCmd.Flags().StringVar(&metricsKind, "kind", "pagerank", "Metric kind: pagerank|hub|authority|in_degree|out_degree|eigenvector|betweenness|cycles|topo|ca|ce|coupling")
	metricsCmd.Flags().StringVar(&metricsGraph, "graph", "imports", "Graph kind ('imports' or 'calls')")
	metricsCmd.Flags().StringVar(&metricsPkg, "pkg", "", "Filter to a single package (suffix-matches package import path)")
	rootCmd.AddCommand(metricsCmd)
}

func runMetrics(_ *cobra.Command, _ []string) error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Validate --kind. Empty results from ReadTopN signal "not yet populated".
	switch metricsKind {
	case "pagerank", "betweenness", "hits", "hub", "authority",
		"cycles", "topo", "degree", "in_degree", "out_degree", "eigenvector",
		"ca", "ce", "coupling":
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
	// Intentionally not supported on the call graph: recursion is normal in code,
	// so a cycle witness on calls isn't useful (use --kind=cycles instead).
	if metricsKind == "topo" {
		if metricsGraph != "imports" {
			return w.WriteError("metrics", &output.Error{
				Code:    output.ErrInternal,
				Message: "topo is only supported for --graph=imports (use --kind=cycles on calls graph)",
			})
		}
		return runTopoMetrics(s, dir, start)
	}

	// Cycles are persisted SCC components — separate output path (not ranked rows).
	if metricsKind == "cycles" {
		return runCyclesMetrics(s, dir, start)
	}

	// Coupling joins ca + ce into a per-package table.
	if metricsKind == "coupling" {
		return runCouplingMetrics(s, dir, start)
	}

	// --pkg implies "show this package only" — load all rows, then filter.
	readN := metricsTopN
	if metricsPkg != "" {
		readN = 0
	}
	rows, err := s.ReadTopN(metricsGraph, metricsKind, readN)
	if err != nil {
		return w.WriteError("metrics", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}
	if metricsPkg != "" {
		rows = filterRowsByPkg(rows, metricsPkg)
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

// filterRowsByPkg keeps rows whose NodeID matches pkg (suffix match against
// the import path so callers can pass either "internal/store" or the fully
// qualified module path).
func filterRowsByPkg(rows []store.MetricRow, pkg string) []store.MetricRow {
	out := rows[:0]
	for _, r := range rows {
		if r.NodeID == pkg || strings.HasSuffix(r.NodeID, "/"+pkg) {
			out = append(out, r)
		}
	}
	return out
}

// couplingRow is the JSON shape for a per-package Ca/Ce/I row.
type couplingRow struct {
	Pkg string  `json:"pkg"`
	Ca  int     `json:"ca"`
	Ce  int     `json:"ce"`
	I   float64 `json:"i"`
}

func runCouplingMetrics(s *store.Store, dir string, startedAt time.Time) error {
	caRows, err := s.ReadTopN("imports", "ca", 0)
	if err != nil {
		return fmt.Errorf("read ca: %w", err)
	}
	ceRows, err := s.ReadTopN("imports", "ce", 0)
	if err != nil {
		return fmt.Errorf("read ce: %w", err)
	}
	if len(caRows) == 0 && len(ceRows) == 0 {
		_, werr := os.Stdout.WriteString("imports graph · coupling · (no rows — run `snipe index` to populate)\n")
		return werr
	}

	byPkg := make(map[string]*couplingRow, len(caRows))
	for _, r := range caRows {
		byPkg[r.NodeID] = &couplingRow{Pkg: r.NodeID, Ca: int(r.Value)}
	}
	for _, r := range ceRows {
		row, ok := byPkg[r.NodeID]
		if !ok {
			row = &couplingRow{Pkg: r.NodeID}
			byPkg[r.NodeID] = row
		}
		row.Ce = int(r.Value)
	}

	rows := make([]couplingRow, 0, len(byPkg))
	for _, r := range byPkg {
		denom := r.Ca + r.Ce
		if denom > 0 {
			r.I = float64(r.Ce) / float64(denom)
		}
		if metricsPkg != "" && r.Pkg != metricsPkg && !strings.HasSuffix(r.Pkg, "/"+metricsPkg) {
			continue
		}
		rows = append(rows, *r)
	}
	// Sort: most-unstable-but-most-coupled first; tiebreak by pkg.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].I != rows[j].I {
			return rows[i].I > rows[j].I
		}
		if rows[i].Ca+rows[i].Ce != rows[j].Ca+rows[j].Ce {
			return rows[i].Ca+rows[i].Ce > rows[j].Ca+rows[j].Ce
		}
		return rows[i].Pkg < rows[j].Pkg
	})
	if metricsTopN > 0 && metricsPkg == "" && len(rows) > metricsTopN {
		rows = rows[:metricsTopN]
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[couplingRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  "metrics",
				Query:    map[string]string{"graph": "imports", "kind": "coupling", "pkg": metricsPkg},
				RepoRoot: dir,
				Ms:       time.Since(startedAt).Milliseconds(),
				Total:    len(rows),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "imports graph · coupling · %d packages\n", len(rows))
	fmt.Fprintf(&b, "  %4s %4s %5s  %s\n", "Ca", "Ce", "I", "pkg")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %4d %4d %5.2f  %s\n", r.Ca, r.Ce, r.I, r.Pkg)
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
