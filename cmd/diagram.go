package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/diagram"
	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/lifecycle"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// Default curation knobs. Each subcommand exposes flags to override.
const (
	defaultDiagramArchTopN   = 20
	defaultDiagramFlowDepth  = 3
	defaultDiagramFlowFanout = 6
	defaultDiagramFlowTopN   = 0 // 0 = no PageRank trim
)

var (
	diagramFormat     string // "d2" (default) or "svg"
	diagramArchTopN   int
	diagramFlowDepth  int
	diagramFlowFanout int
	diagramFlowTopN   int
)

var diagramCmd = &cobra.Command{
	Use:     "diagram",
	Short:   "Render snipe graphs as D2 diagram source",
	GroupID: "advanced",
	Long: `Emit D2 (https://d2lang.com) source from snipe's import, call, and lifecycle graphs.

Three opinionated subcommands:
  snipe diagram arch                Package layering with role annotations
  snipe diagram flow <entry>        Depth-limited call graph from one symbol
  snipe diagram lifecycle <Type>    CRUD lifecycle data as D2

Default output is a Claude-readable summary followed by the D2 source.
Use --format=svg to shell out to the d2 CLI (must be installed).`,
}

var diagramArchCmd = &cobra.Command{
	Use:   "arch",
	Short: "Package import graph trimmed to the top-N by PageRank, grouped by layer",
	Args:  cobra.NoArgs,
	RunE:  runDiagramArch,
}

var diagramFlowCmd = &cobra.Command{
	Use:   "flow <entry>",
	Short: "Depth-limited call graph from an entry symbol",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiagramFlow,
}

var diagramLifecycleCmd = &cobra.Command{
	Use:   "lifecycle <Type>",
	Short: "Render lifecycle CRUD groups for a type as D2",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiagramLifecycle,
}

func init() {
	diagramCmd.PersistentFlags().StringVar(&diagramFormat, "format", "d2",
		"Output format: 'd2' (default, source) or 'svg' (shells out to d2 CLI)")

	diagramArchCmd.Flags().IntVar(&diagramArchTopN, "top", defaultDiagramArchTopN,
		"Trim graph to top-N packages by import-graph PageRank (0 = no trim)")

	diagramFlowCmd.Flags().IntVar(&diagramFlowDepth, "depth", defaultDiagramFlowDepth,
		"Maximum BFS depth from entry (1..10)")
	diagramFlowCmd.Flags().IntVar(&diagramFlowFanout, "fanout", defaultDiagramFlowFanout,
		"Cap callees expanded per node (prevents hairballs)")
	diagramFlowCmd.Flags().IntVar(&diagramFlowTopN, "top-n", defaultDiagramFlowTopN,
		"After BFS, intersect with top-N by call-graph PageRank (0 = no trim; ignored if metrics not populated)")

	diagramCmd.AddCommand(diagramArchCmd, diagramFlowCmd, diagramLifecycleCmd)
	rootCmd.AddCommand(diagramCmd)
}

// emit prints the Claude summary and D2 source, or shells out to `d2` for svg.
// summary is plain text describing the diagram (D1: this is what Claude reads).
// d2src is the D2 program.
func emit(summary, d2src string) error {
	switch diagramFormat {
	case "", "d2":
		// Default: text summary first, then a fenced D2 block as a follow-up.
		fmt.Print(summary)
		if !strings.HasSuffix(summary, "\n") {
			fmt.Println()
		}
		fmt.Println()
		fmt.Println("```d2")
		fmt.Print(d2src)
		if !strings.HasSuffix(d2src, "\n") {
			fmt.Println()
		}
		fmt.Println("```")
		return nil
	case "svg":
		return renderSVG(d2src)
	default:
		return fmt.Errorf("unknown --format %q (want d2 or svg)", diagramFormat)
	}
}

func renderSVG(d2src string) error {
	bin, err := exec.LookPath("d2")
	if err != nil {
		return fmt.Errorf("d2 CLI not found in PATH (install from https://d2lang.com); falling back: rerun without --format=svg to get D2 source")
	}
	c := exec.Command(bin, "-", "-") // #nosec G204 -- bin resolved via LookPath
	c.Stdin = strings.NewReader(d2src)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("d2 render failed: %w", err)
	}
	return nil
}

// ---------- arch ----------------------------------------------------------

func runDiagramArch(_ *cobra.Command, _ []string) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram arch")
	if err != nil {
		return err
	}
	defer s.Close()

	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}

	// Trim by top-N PageRank when populated, else keep full graph.
	keep, ranking, err := graphmetrics.TopNByPageRank(s, "imports", diagramArchTopN)
	if err != nil {
		return err
	}

	pkgs, edges := filterGraph(g, keep)

	// Group packages by leading internal layer (first segment after the
	// module path). e.g. "github.com/dkoosis/snipe/internal/store" -> "internal".
	module := stripLastSeg(pkgsCommonPrefix(pkgs))

	var b diagram.Builder
	b.Title = "snipe arch · imports graph"
	b.Direction = "down"

	containerStyle := map[string]string{"fill": "#f5f5f5"}
	for _, p := range pkgs {
		layer := layerOf(p, module)
		container := "layer_" + diagram.SanitizeID(layer)
		b.AddContainer(container, layer, containerStyle)
		short := shortPkg(p, module)
		nodeID := container + "." + diagram.SanitizeID(short)
		style := nodeStyleForRank(ranking[p])
		b.AddNode(nodeID, short, "", style)
	}
	for _, e := range edges {
		fromLayer := "layer_" + diagram.SanitizeID(layerOf(e[0], module))
		toLayer := "layer_" + diagram.SanitizeID(layerOf(e[1], module))
		from := fromLayer + "." + diagram.SanitizeID(shortPkg(e[0], module))
		to := toLayer + "." + diagram.SanitizeID(shortPkg(e[1], module))
		b.AddEdge(from, to, "", nil)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "diagram arch · %d packages · %d edges", len(pkgs), len(edges))
	if diagramArchTopN > 0 && len(ranking) > 0 {
		fmt.Fprintf(&sb, " · trimmed to top-%d by PageRank", diagramArchTopN)
	} else {
		sb.WriteString(" · no PageRank trim (run `snipe index` to populate)")
	}
	if module != "" {
		fmt.Fprintf(&sb, " · module=%s", module)
	}
	sb.WriteString("\n")
	for _, p := range pkgs {
		fmt.Fprintf(&sb, "  %s  (layer=%s)\n", shortPkg(p, module), layerOf(p, module))
	}

	return emit(sb.String(), b.Render())
}

func filterGraph(g *graphmetrics.Graph, keep map[string]bool) ([]string, [][2]string) {
	var nodes []string
	if keep == nil {
		nodes = g.Nodes()
	} else {
		for _, n := range g.Nodes() {
			if keep[n] {
				nodes = append(nodes, n)
			}
		}
	}
	sort.Strings(nodes)

	in := func(n string) bool { return keep == nil || keep[n] }
	var edges [][2]string
	for _, src := range nodes {
		for _, dst := range g.OutEdges(src) {
			if !in(dst) {
				continue
			}
			edges = append(edges, [2]string{src, dst})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i][0] != edges[j][0] {
			return edges[i][0] < edges[j][0]
		}
		return edges[i][1] < edges[j][1]
	})
	return nodes, edges
}

// nodeStyleForRank highlights the top-3 packages so visual hierarchy matches
// PageRank centrality. Lower rank = higher centrality.
func nodeStyleForRank(rank int) map[string]string {
	switch {
	case rank == 0:
		return nil
	case rank <= 3:
		return map[string]string{"fill": "#fde68a", "bold": "true"}
	case rank <= 10:
		return map[string]string{"fill": "#fef3c7"}
	default:
		return nil
	}
}

// layerOf returns a coarse grouping label for a Go package path.
// "<module>/internal/store/embed" -> "internal/store"
// "<module>/cmd"                  -> "cmd"
// "<module>"                      -> "root"
func layerOf(pkg, module string) string {
	rest := strings.TrimPrefix(pkg, module)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "root"
	}
	parts := strings.Split(rest, "/")
	switch parts[0] {
	case "internal":
		if len(parts) >= 2 {
			return "internal/" + parts[1]
		}
		return "internal"
	default:
		return parts[0]
	}
}

func shortPkg(pkg, module string) string {
	rest := strings.TrimPrefix(pkg, module)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return path.Base(pkg)
	}
	return rest
}

// pkgsCommonPrefix returns the longest "/"-bounded common prefix across pkgs.
// Used as a heuristic for the module path when go.mod isn't read here.
func pkgsCommonPrefix(pkgs []string) string {
	if len(pkgs) == 0 {
		return ""
	}
	prefix := pkgs[0]
	for _, p := range pkgs[1:] {
		prefix = commonPathPrefix(prefix, p)
		if prefix == "" {
			break
		}
	}
	return prefix
}

func commonPathPrefix(a, b string) string {
	as := strings.Split(a, "/")
	bs := strings.Split(b, "/")
	n := len(as)
	if len(bs) < n {
		n = len(bs)
	}
	var out []string
	for i := 0; i < n; i++ {
		if as[i] != bs[i] {
			break
		}
		out = append(out, as[i])
	}
	return strings.Join(out, "/")
}

func stripLastSeg(p string) string { return p }

// ---------- flow ----------------------------------------------------------

func runDiagramFlow(_ *cobra.Command, args []string) error {
	if diagramFlowDepth < 1 {
		diagramFlowDepth = 1
	}
	if diagramFlowDepth > 10 {
		diagramFlowDepth = 10
	}

	w := output.NewWriter(os.Stdout, false, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram flow")
	if err != nil {
		return err
	}
	defer s.Close()

	entry := args[0]
	syms, err := query.LookupByName(s.DB(), entry)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", entry, err)
	}
	if len(syms) == 0 {
		return fmt.Errorf("symbol %q not found (try 'snipe search' to confirm name)", entry)
	}
	root := syms[0]
	if len(syms) > 1 {
		// Prefer functions/methods for flow diagrams.
		for _, c := range syms {
			if c.Kind == "func" || c.Kind == "method" {
				root = c
				break
			}
		}
	}

	g, err := graphmetrics.LoadCallsGraph(s)
	if err != nil {
		return fmt.Errorf("load call graph: %w", err)
	}

	visited, edges := bfsCallees(g, root.ID, diagramFlowDepth, diagramFlowFanout)

	// Optional curation: intersect BFS result with top-N by call-graph PageRank.
	// Falls back to the un-trimmed BFS when metrics aren't populated.
	var trimNote string
	preTrimNodes := len(visited)
	preTrimEdges := len(edges)
	if diagramFlowTopN > 0 {
		keep, _, terr := graphmetrics.TopNByPageRank(s, "calls", diagramFlowTopN)
		if terr != nil {
			return terr
		}
		if keep == nil {
			trimNote = fmt.Sprintf(" · --top-n=%d requested but graph_metrics empty (run `snipe metrics --graph=calls --kind=pagerank`)", diagramFlowTopN)
		} else {
			// Always retain the entry node so the diagram has an anchor.
			keep[root.ID] = true
			visited, edges = filterFlow(visited, edges, keep)
			trimNote = fmt.Sprintf(" · trimmed to top-%d by PageRank (was %d nodes / %d edges)",
				diagramFlowTopN, preTrimNodes, preTrimEdges)
		}
	}

	// Resolve symbol metadata in one batch.
	ids := make([]string, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	syMap, err := query.BatchLookupByID(s.DB(), ids)
	if err != nil {
		return fmt.Errorf("batch lookup: %w", err)
	}

	var b diagram.Builder
	b.Title = fmt.Sprintf("snipe flow · %s · depth=%d", root.Name, diagramFlowDepth)
	b.Direction = "right"

	for id := range visited {
		label := id // fallback
		if sym, ok := syMap[id]; ok && sym != nil {
			label = displayLabel(sym)
		}
		nodeID := diagram.SanitizeID(id)
		style := map[string]string{}
		if id == root.ID {
			style["fill"] = "#fde68a"
			style["bold"] = "true"
		}
		if len(style) == 0 {
			style = nil
		}
		b.AddNode(nodeID, label, "", style)
	}
	for _, e := range edges {
		from := diagram.SanitizeID(e[0])
		to := diagram.SanitizeID(e[1])
		b.AddEdge(from, to, "", nil)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "diagram flow · entry=%s · %d nodes · %d edges · depth=%d · fanout=%d%s\n",
		root.Name, len(visited), len(edges), diagramFlowDepth, diagramFlowFanout, trimNote)
	// Brief textual outline.
	roots := []string{root.ID}
	written := map[string]bool{}
	walk(roots, edges, 0, diagramFlowDepth, syMap, &sb, written)

	return emit(sb.String(), b.Render())
}

func displayLabel(sym *query.SymbolRow) string {
	if sym.Receiver.Valid && sym.Receiver.String != "" {
		return sym.Receiver.String + "." + sym.Name
	}
	return sym.Name
}

// filterFlow restricts a BFS result (visited set + edges) to nodes in keep.
// Edges are retained only when both endpoints survive.
func filterFlow(visited map[string]bool, edges [][2]string, keep map[string]bool) (map[string]bool, [][2]string) {
	out := make(map[string]bool, len(visited))
	for id := range visited {
		if keep[id] {
			out[id] = true
		}
	}
	kept := make([][2]string, 0, len(edges))
	for _, e := range edges {
		if out[e[0]] && out[e[1]] {
			kept = append(kept, e)
		}
	}
	return out, kept
}

// bfsCallees walks caller -> callee edges from root. fanout caps the number
// of children expanded from each node (deterministic by sorted callee id).
func bfsCallees(g *graphmetrics.Graph, root string, depth, fanout int) (map[string]bool, [][2]string) {
	visited := map[string]bool{root: true}
	var edges [][2]string
	type item struct {
		id    string
		depth int
	}
	queue := []item{{root, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= depth {
			continue
		}
		callees := g.OutEdges(cur.id)
		if fanout > 0 && len(callees) > fanout {
			callees = callees[:fanout]
		}
		for _, c := range callees {
			edges = append(edges, [2]string{cur.id, c})
			if !visited[c] {
				visited[c] = true
				queue = append(queue, item{c, cur.depth + 1})
			}
		}
	}
	return visited, edges
}

func walk(roots []string, edges [][2]string, depth, maxDepth int, syMap map[string]*query.SymbolRow, sb *strings.Builder, written map[string]bool) {
	if depth > maxDepth {
		return
	}
	for _, r := range roots {
		if written[r] {
			continue
		}
		written[r] = true
		label := r
		if sym, ok := syMap[r]; ok && sym != nil {
			label = displayLabel(sym)
			if sym.FilePathRel != "" {
				label = fmt.Sprintf("%s  %s:%d", label, sym.FilePathRel, sym.LineStart)
			}
		}
		fmt.Fprintf(sb, "  %s%s\n", strings.Repeat("  ", depth), label)
		var kids []string
		for _, e := range edges {
			if e[0] == r {
				kids = append(kids, e[1])
			}
		}
		walk(kids, edges, depth+1, maxDepth, syMap, sb, written)
	}
}

// ---------- lifecycle ----------------------------------------------------

func runDiagramLifecycle(_ *cobra.Command, args []string) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram lifecycle")
	if err != nil {
		return err
	}
	defer s.Close()

	typeName := args[0]
	syms, err := query.LookupByName(s.DB(), typeName)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", typeName, err)
	}
	if len(syms) == 0 {
		return fmt.Errorf("type %q not found", typeName)
	}
	if len(syms) > 1 {
		return errors.New("ambiguous type — use a more specific name")
	}
	sym := syms[0]

	refRows, err := query.FindRefs(s.DB(), sym.ID, 10000, 0)
	if err != nil {
		return fmt.Errorf("find refs: %w", err)
	}
	reattachSignatureRefs(s.DB(), refRows)

	refs := lifecycle.FromRefRows(refRows)
	classifications := lifecycle.Classify(typeName, refs)

	// Bucket by role.
	type fnEntry struct {
		id, name, file string
		line           int
	}
	buckets := map[lifecycle.Role][]fnEntry{}
	for _, c := range classifications {
		if c.IsTestFile {
			continue
		}
		buckets[c.Role] = append(buckets[c.Role], fnEntry{
			id:   c.EnclosingID,
			name: displayName(c.EnclosingName),
			file: c.FileRel,
			line: c.Line,
		})
	}

	var b diagram.Builder
	b.Title = fmt.Sprintf("snipe lifecycle · %s", typeName)
	b.Direction = "right"

	// Center node: the type.
	typeNodeID := "type_" + diagram.SanitizeID(sym.Name)
	b.AddNode(typeNodeID, sym.Name, "cylinder", map[string]string{"fill": "#fde68a", "bold": "true"})

	// One container per role with role-specific tint.
	roleColors := map[lifecycle.Role]string{
		lifecycle.RoleCreate:  "#dcfce7",
		lifecycle.RoleMutate:  "#fef9c3",
		lifecycle.RoleRead:    "#dbeafe",
		lifecycle.RoleDelete:  "#fee2e2",
		lifecycle.RoleUnknown: "#f3f4f6",
	}

	roles := []lifecycle.Role{
		lifecycle.RoleCreate,
		lifecycle.RoleMutate,
		lifecycle.RoleRead,
		lifecycle.RoleDelete,
	}
	if len(buckets[lifecycle.RoleUnknown]) > 0 {
		roles = append(roles, lifecycle.RoleUnknown)
	}

	totalFns := 0
	for _, role := range roles {
		fns := buckets[role]
		if len(fns) == 0 {
			continue
		}
		container := "role_" + diagram.SanitizeID(string(role))
		b.AddContainer(container, string(role), map[string]string{"fill": roleColors[role]})
		// Cap per role so a busy struct doesn't explode the diagram.
		const perRoleCap = 8
		shown := fns
		truncated := 0
		if len(shown) > perRoleCap {
			truncated = len(shown) - perRoleCap
			shown = shown[:perRoleCap]
		}
		for _, fn := range shown {
			id := container + "." + diagram.SanitizeID(fn.id)
			label := fmt.Sprintf("%s · %s:%d", fn.name, fn.file, fn.line)
			b.AddNode(id, label, "", nil)
			edgeFromType(&b, role, typeNodeID, id)
			totalFns++
		}
		if truncated > 0 {
			id := container + "._more"
			b.AddNode(id, fmt.Sprintf("+%d more", truncated), "", map[string]string{"font-color": "#6b7280"})
		}
	}

	roleCount := 0
	for _, role := range roles {
		if len(buckets[role]) > 0 {
			roleCount++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "diagram lifecycle · type=%s · file=%s:%d · %d funcs across %d roles\n",
		sym.Name, sym.FilePathRel, sym.LineStart, totalFns, roleCount)
	for _, role := range roles {
		fns := buckets[role]
		if len(fns) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "  %s (%d)\n", role, len(fns))
	}

	return emit(sb.String(), b.Render())
}

// edgeFromType picks edge direction depending on role: Create/Mutate/Delete
// flow function -> type (the function operates on it), Read flows the other
// direction. Visual cue, not semantic.
func edgeFromType(b *diagram.Builder, role lifecycle.Role, typeNode, fnNode string) {
	switch role {
	case lifecycle.RoleRead:
		b.AddEdge(typeNode, fnNode, "read", nil)
	case lifecycle.RoleCreate:
		b.AddEdge(fnNode, typeNode, "create", nil)
	case lifecycle.RoleMutate:
		b.AddEdge(fnNode, typeNode, "mutate", nil)
	case lifecycle.RoleDelete:
		b.AddEdge(fnNode, typeNode, "delete", nil)
	case lifecycle.RoleUnknown:
		b.AddDashedEdge(fnNode, typeNode, string(role))
	default:
		b.AddDashedEdge(fnNode, typeNode, string(role))
	}
}
