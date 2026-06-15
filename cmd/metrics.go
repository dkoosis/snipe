package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

const kindLCOM4 = "lcom4"

var (
	metricsTopN  int
	metricsKind  string
	metricsGraph string
	metricsPkg   string
)

func runMetrics() error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Multi-kind: comma-separated list collapses N sequential calls into one
	// merged table keyed by node (snipe-0zg). Composite kinds (topo, cycles,
	// coupling, distance, cyclo) are excluded — they have non-row outputs.
	if strings.Contains(metricsKind, ",") {
		s, dir, err := OpenStore(w, cmdNameMetrics)
		if err != nil {
			return err
		}
		defer s.Close()
		return runMultiKindMetrics(s, dir, start)
	}

	// Usage telemetry reads .snipe/usage.jsonl, not the index — handle first.
	if metricsKind == kindUsage {
		return runUsageMetrics()
	}

	// Validate --kind. Empty results from ReadTopN signal "not yet populated".
	switch metricsKind {
	case "pagerank", "betweenness", "hits", "hub", "authority",
		cmdKindCycles, cmdKindTopo, "degree", "in_degree", "out_degree", "eigenvector",
		"ca", "ce", cmdKindCoupling, "instability", "abstractness", cmdKindDistance, kindLCOM4,
		cmdKindCyclo, "cyclo_sum", "cyclo_p95", "cyclo_max":
		// ok
	default:
		return w.WriteError(cmdNameMetrics, &output.Error{
			Code:    output.ErrInternal,
			Message: fmt.Sprintf("unknown --kind %q", metricsKind),
		})
	}

	s, dir, err := OpenStore(w, cmdNameMetrics)
	if err != nil {
		return err
	}
	defer s.Close()

	// Topo sort is transient — recomputed on demand from the imports graph.
	// Intentionally not supported on the call graph: recursion is normal in code,
	// so a cycle witness on calls isn't useful (use --kind=cycles instead).
	if metricsKind == cmdKindTopo {
		if metricsGraph != cmdNameImports {
			return w.WriteError(cmdNameMetrics, &output.Error{
				Code:    output.ErrInternal,
				Message: "topo is only supported for --graph=imports (use --kind=cycles on calls graph)",
			})
		}
		return runTopoMetrics(s, dir, start)
	}

	// Cycles are persisted SCC components — separate output path (not ranked rows).
	if metricsKind == cmdKindCycles {
		return runCyclesMetrics(s, dir, start)
	}

	// Coupling joins ca + ce into a per-package table.
	if metricsKind == cmdKindCoupling {
		return runCouplingMetrics(s, dir, start)
	}

	// Distance joins abstractness + instability into D = |A + I - 1|.
	if metricsKind == cmdKindDistance {
		return runDistanceMetrics(s, dir, start)
	}

	// Cyclo joins per-package sum/p95/max into a hot-package table.
	if metricsKind == cmdKindCyclo {
		return runCycloMetrics(s, dir, start)
	}

	// --pkg implies "show this package only" — load all rows, then filter.
	readN := metricsTopN
	if metricsPkg != "" {
		readN = 0
	}
	rows, err := s.ReadTopN(metricsGraph, metricsKind, readN)
	if err != nil {
		return w.WriteError(cmdNameMetrics, &output.Error{
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
			Command:  cmdNameMetrics,
			Query:    map[string]string{cmdKindGraph: metricsGraph, jsonKeyKind: metricsKind, "top": fmt.Sprintf("%d", metricsTopN)},
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
				Command:  cmdNameMetrics,
				Query:    map[string]string{cmdKindGraph: metricsGraph, jsonKeyKind: cmdKindTopo},
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
	caRows, err := s.ReadTopN(cmdNameImports, "ca", 0)
	if err != nil {
		return fmt.Errorf("read ca: %w", err)
	}
	ceRows, err := s.ReadTopN(cmdNameImports, "ce", 0)
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
				Command:  cmdNameMetrics,
				Query:    map[string]string{cmdKindGraph: cmdNameImports, jsonKeyKind: cmdKindCoupling, flagPkg: metricsPkg},
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
	fmt.Fprintf(&b, "  %4s %4s %5s  %s\n", "Ca", "Ce", "I", flagPkg)
	for _, r := range rows {
		fmt.Fprintf(&b, "  %4d %4d %5.2f  %s\n", r.Ca, r.Ce, r.I, r.Pkg)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

// distanceRow is the JSON shape for a per-package A/I/D row.
//
// D = |A + I - 1| measures distance from Martin's "main sequence":
// D≈0 = healthy (concrete-stable utility OR abstract-unstable abstraction);
// D→1 = painful (concrete-unstable junk drawer at A=0,I=1 OR abstract-stable
// orphan interface at A=1,I=0). D > 0.7 flags a package for arch review.
type distanceRow struct {
	Pkg string  `json:"pkg"`
	A   float64 `json:"a"`
	I   float64 `json:"i"`
	D   float64 `json:"d"`
}

func runDistanceMetrics(s *store.Store, dir string, startedAt time.Time) error {
	aRows, err := s.ReadTopN(cmdNameImports, "abstractness", 0)
	if err != nil {
		return fmt.Errorf("read abstractness: %w", err)
	}
	iRows, err := s.ReadTopN(cmdNameImports, "instability", 0)
	if err != nil {
		return fmt.Errorf("read instability: %w", err)
	}
	if len(aRows) == 0 && len(iRows) == 0 {
		_, werr := os.Stdout.WriteString("imports graph · distance · (no rows — run `snipe index` to populate)\n")
		return werr
	}

	// Join on pkg. Packages without an abstractness row (zero-type pkgs) are
	// skipped — A is undefined for them.
	byPkg := make(map[string]*distanceRow, len(aRows))
	for _, r := range aRows {
		byPkg[r.NodeID] = &distanceRow{Pkg: r.NodeID, A: r.Value}
	}
	for _, r := range iRows {
		row, ok := byPkg[r.NodeID]
		if !ok {
			continue
		}
		row.I = r.Value
	}

	rows := make([]distanceRow, 0, len(byPkg))
	for _, r := range byPkg {
		sum := r.A + r.I - 1
		if sum < 0 {
			sum = -sum
		}
		r.D = sum
		if metricsPkg != "" && r.Pkg != metricsPkg && !strings.HasSuffix(r.Pkg, "/"+metricsPkg) {
			continue
		}
		rows = append(rows, *r)
	}
	// Sort: highest D (worst) first; tiebreak by pkg.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].D != rows[j].D {
			return rows[i].D > rows[j].D
		}
		return rows[i].Pkg < rows[j].Pkg
	})
	if metricsTopN > 0 && metricsPkg == "" && len(rows) > metricsTopN {
		rows = rows[:metricsTopN]
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[distanceRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  cmdNameMetrics,
				Query:    map[string]string{cmdKindGraph: cmdNameImports, jsonKeyKind: cmdKindDistance, flagPkg: metricsPkg},
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
	fmt.Fprintf(&b, "imports graph · distance · %d packages (D > 0.70 = arch-review flag)\n", len(rows))
	fmt.Fprintf(&b, "  %5s %5s %5s  %s\n", "A", "I", "D", flagPkg)
	for _, r := range rows {
		flag := "  "
		if r.D > 0.70 {
			flag = "! "
		}
		fmt.Fprintf(&b, "%s%5.2f %5.2f %5.2f  %s\n", flag, r.A, r.I, r.D, r.Pkg)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

// cycloRow is the JSON shape for a per-package cyclomatic-complexity row.
// Sum/Max are integers; P95 may interpolate between samples.
// Hot-package threshold (per snipe-fac AC): p95 > 10 OR max > 20.
type cycloRow struct {
	Pkg string  `json:"pkg"`
	Sum int     `json:"sum"`
	P95 float64 `json:"p95"`
	Max int     `json:"max"`
}

func runCycloMetrics(s *store.Store, dir string, startedAt time.Time) error {
	sumRows, err := s.ReadTopN(cmdNameImports, "cyclo_sum", 0)
	if err != nil {
		return fmt.Errorf("read cyclo_sum: %w", err)
	}
	p95Rows, err := s.ReadTopN(cmdNameImports, "cyclo_p95", 0)
	if err != nil {
		return fmt.Errorf("read cyclo_p95: %w", err)
	}
	maxRows, err := s.ReadTopN(cmdNameImports, "cyclo_max", 0)
	if err != nil {
		return fmt.Errorf("read cyclo_max: %w", err)
	}
	if len(sumRows) == 0 && len(p95Rows) == 0 && len(maxRows) == 0 {
		_, werr := os.Stdout.WriteString("imports graph · cyclo · (no rows — run `snipe index` to populate)\n")
		return werr
	}

	byPkg := make(map[string]*cycloRow, len(sumRows))
	for _, r := range sumRows {
		byPkg[r.NodeID] = &cycloRow{Pkg: r.NodeID, Sum: int(r.Value)}
	}
	for _, r := range p95Rows {
		row, ok := byPkg[r.NodeID]
		if !ok {
			row = &cycloRow{Pkg: r.NodeID}
			byPkg[r.NodeID] = row
		}
		row.P95 = r.Value
	}
	for _, r := range maxRows {
		row, ok := byPkg[r.NodeID]
		if !ok {
			row = &cycloRow{Pkg: r.NodeID}
			byPkg[r.NodeID] = row
		}
		row.Max = int(r.Value)
	}

	rows := make([]cycloRow, 0, len(byPkg))
	for _, r := range byPkg {
		if metricsPkg != "" && r.Pkg != metricsPkg && !strings.HasSuffix(r.Pkg, "/"+metricsPkg) {
			continue
		}
		rows = append(rows, *r)
	}
	// Sort: highest sum first (hottest packages); tiebreak by max then pkg.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Sum != rows[j].Sum {
			return rows[i].Sum > rows[j].Sum
		}
		if rows[i].Max != rows[j].Max {
			return rows[i].Max > rows[j].Max
		}
		return rows[i].Pkg < rows[j].Pkg
	})
	if metricsTopN > 0 && metricsPkg == "" && len(rows) > metricsTopN {
		rows = rows[:metricsTopN]
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[cycloRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  cmdNameMetrics,
				Query:    map[string]string{cmdKindGraph: cmdNameImports, jsonKeyKind: cmdKindCyclo, flagPkg: metricsPkg},
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
	fmt.Fprintf(&b, "imports graph · cyclo · %d packages (p95 > 10 OR max > 20 = hot-package flag)\n", len(rows))
	fmt.Fprintf(&b, "  %5s %5s %5s  %s\n", "sum", "p95", "max", flagPkg)
	for _, r := range rows {
		flag := "  "
		if r.P95 > 10 || r.Max > 20 {
			flag = "! "
		}
		fmt.Fprintf(&b, "%s%5d %5.1f %5d  %s\n", flag, r.Sum, r.P95, r.Max, r.Pkg)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

func writeMetricsText(rows []store.MetricRow) error {
	var b strings.Builder
	header := fmt.Sprintf("%s graph · %s · top %d", metricsGraph, metricsKind, len(rows))
	if metricsKind == kindLCOM4 {
		header += " (LCOM4 > 1 = split candidate; cross-ref `snipe boundary`)"
	}
	fmt.Fprintln(&b, header)
	if len(rows) == 0 {
		b.WriteString("  (no metric rows — run `snipe index` to populate)\n")
	}
	for _, r := range rows {
		switch metricsKind {
		case kindLCOM4, "ca", "ce", "in_degree", "out_degree":
			fmt.Fprintf(&b, "  %2d  %5d  %s\n", r.Rank, int(r.Value), r.NodeID)
		default:
			fmt.Fprintf(&b, "  %2d  %.3f  %s\n", r.Rank, r.Value, r.NodeID)
		}
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}

// multiKindRow is one node with its values across the requested kinds.
type multiKindRow struct {
	Node   string             `json:"node"`
	Values map[string]float64 `json:"values"`
}

// runMultiKindMetrics handles the comma-separated --kind path: read each kind
// row-set, merge by NodeID into a single wide table, optionally filter by
// --pkg, sort by the first kind value desc, then truncate to --top.
func runMultiKindMetrics(s *store.Store, dir string, startedAt time.Time) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	kinds := splitAndTrim(metricsKind)
	if len(kinds) == 0 {
		return w.WriteError(cmdNameMetrics, &output.Error{
			Code: output.ErrInternal, Message: "empty --kind list",
		})
	}
	for _, k := range kinds {
		switch k {
		case "pagerank", "betweenness", "hits", "hub", "authority",
			"degree", "in_degree", "out_degree", "eigenvector",
			"ca", "ce", "instability", "abstractness", kindLCOM4,
			"cyclo_sum", "cyclo_p95", "cyclo_max":
		default:
			return w.WriteError(cmdNameMetrics, &output.Error{
				Code: output.ErrInternal,
				Message: fmt.Sprintf(
					"--kind=%q not supported in multi-kind mode (composite kinds topo/cycles/coupling/distance/cyclo must run alone)",
					k,
				),
			})
		}
	}

	byNode := make(map[string]map[string]float64)
	for _, k := range kinds {
		rows, err := s.ReadTopN(metricsGraph, k, 0)
		if err != nil {
			return w.WriteError(cmdNameMetrics, &output.Error{
				Code: output.ErrInternal, Message: err.Error(),
			})
		}
		for _, r := range rows {
			if metricsPkg != "" && r.NodeID != metricsPkg && !strings.HasSuffix(r.NodeID, "/"+metricsPkg) {
				continue
			}
			vals, ok := byNode[r.NodeID]
			if !ok {
				vals = make(map[string]float64, len(kinds))
				byNode[r.NodeID] = vals
			}
			vals[k] = r.Value
		}
	}

	out := make([]multiKindRow, 0, len(byNode))
	for node, vals := range byNode {
		out = append(out, multiKindRow{Node: node, Values: vals})
	}
	first := kinds[0]
	sort.Slice(out, func(i, j int) bool {
		vi := out[i].Values[first]
		vj := out[j].Values[first]
		if vi != vj {
			return vi > vj
		}
		return out[i].Node < out[j].Node
	})
	if metricsTopN > 0 && len(out) > metricsTopN {
		out = out[:metricsTopN]
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[multiKindRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  out,
			Meta: output.Meta{
				Command: cmdNameMetrics,
				Query: map[string]string{
					cmdKindGraph: metricsGraph,
					jsonKeyKind:  metricsKind,
					"top":        fmt.Sprintf("%d", metricsTopN),
				},
				RepoRoot: dir,
				Ms:       time.Since(startedAt).Milliseconds(),
				Total:    len(out),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s graph · %s · %d rows\n", metricsGraph, strings.Join(kinds, ","), len(out))
	b.WriteString("  node")
	for _, k := range kinds {
		fmt.Fprintf(&b, "\t%s", k)
	}
	b.WriteString("\n")
	for _, r := range out {
		fmt.Fprintf(&b, "  %s", r.Node)
		for _, k := range kinds {
			fmt.Fprintf(&b, "\t%g", r.Values[k])
		}
		b.WriteString("\n")
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}

func splitAndTrim(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
