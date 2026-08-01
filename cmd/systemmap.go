package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/diagram"
	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/output"
)

// systemMapTopN mirrors diagramArchTopN's role for `diagram system`: 0 means
// no PageRank trim (the default — a comprehension view wants every package
// accounted for somewhere, even if collapsed into a small subsystem).
var systemMapTopN int

// Subsystem names — the handful of "main parts of the system" that
// subsystemOf buckets packages into. Named constants (not inline literals)
// because each appears in both subsystemOf and its test table (goconst).
const (
	subsystemCLI          = "CLI"
	subsystemRoot         = "Root"
	subsystemIndexing     = "Indexing & Analysis"
	subsystemPersistence  = "Persistence"
	subsystemQuery        = "Query"
	subsystemPresentation = "Presentation"
	subsystemSupport      = "Support"
)

// ---------- system map ------------------------------------------------------
//
// sn-l1kh.2: a comprehension view answering "what are the main parts of this
// system and how do they relate", distinct from `diagram arch` (a diagnostic
// view of all packages flat, grouped only visually by layer container).
// System map collapses each subsystem to a single node and collapses
// intra-subsystem edges entirely — only cross-subsystem relationships are
// drawn. Reuses arch's graph-loading path (LoadImportsGraph, TopNByPageRank,
// filterGraph) rather than re-deriving package/edge data.

func runDiagramSystem() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram system-map")
	if err != nil {
		return err
	}
	defer s.Close()

	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}

	keep, ranking, err := graphmetrics.TopNByPageRank(s, "imports", systemMapTopN)
	if err != nil {
		return err
	}

	pkgs, edges := filterGraph(g, keep)
	module := stripLastSeg(pkgsCommonPrefix(pkgs))

	groups, groupEdges := groupSystemMap(pkgs, edges, module)

	var b diagram.Builder
	b.Title = "snipe system map · subsystems"
	b.Direction = "down"

	for _, gr := range groups {
		style := map[string]string{diagramFill: "#f5f5f5"}
		if hasHighRank(gr.pkgs, ranking) {
			style = map[string]string{diagramFill: diagramColorWarn, diagramBold: diagramTrue}
		}
		label := fmt.Sprintf("%s (%d)", gr.name, len(gr.pkgs))
		b.AddNode(systemNodeID(gr.name), label, "", style)
	}
	for _, e := range groupEdges {
		label := ""
		if e.count > 1 {
			label = fmt.Sprintf("%d", e.count)
		}
		b.AddEdge(systemNodeID(e.from), systemNodeID(e.to), label, nil)
	}

	sb := renderSystemMapSummary(groups, groupEdges, pkgs, edges, systemMapTopN, ranking, module)

	return emitDoc("system-map", sb, b.Render())
}

// systemGroup is one collapsed subsystem: its display name and member
// packages (full import paths, sorted).
type systemGroup struct {
	name string
	pkgs []string
}

// groupEdge is a collapsed, deduplicated cross-subsystem relationship. count
// is the number of underlying package-level edges it represents.
type groupEdge struct {
	from, to string
	count    int
}

// groupSystemMap buckets pkgs into subsystems via subsystemOf, then collapses
// edges: intra-subsystem edges are dropped entirely, cross-subsystem edges
// are deduplicated into one groupEdge per (from,to) pair with a count of how
// many package-level edges it summarizes. Groups and edges are returned in a
// deterministic (sorted) order.
func groupSystemMap(pkgs []string, edges [][2]string, module string) ([]systemGroup, []groupEdge) {
	members := map[string][]string{}
	pkgGroup := map[string]string{}
	for _, p := range pkgs {
		layer := layerOf(p, module)
		name := subsystemOf(layer)
		members[name] = append(members[name], p)
		pkgGroup[p] = name
	}

	var groups []systemGroup
	for name, ps := range members {
		sort.Strings(ps)
		groups = append(groups, systemGroup{name: name, pkgs: ps})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].name < groups[j].name })

	counts := map[[2]string]int{}
	for _, e := range edges {
		from, to := pkgGroup[e[0]], pkgGroup[e[1]]
		if from == "" || to == "" || from == to {
			continue
		}
		counts[[2]string{from, to}]++
	}
	var groupEdges []groupEdge
	for k, c := range counts {
		groupEdges = append(groupEdges, groupEdge{from: k[0], to: k[1], count: c})
	}
	sort.Slice(groupEdges, func(i, j int) bool {
		if groupEdges[i].from != groupEdges[j].from {
			return groupEdges[i].from < groupEdges[j].from
		}
		return groupEdges[i].to < groupEdges[j].to
	})
	return groups, groupEdges
}

// subsystemOf maps a package's arch layer (as computed by layerOf: "cmd",
// "root", or "internal/<name>") to one of a handful of comprehension-level
// "main parts of the system", grouped by naming convention. New internal
// packages that don't match a known theme fall into "Support" rather than
// growing the subsystem count — the map favors a stable, readable handful of
// parts over exhaustively categorizing every package.
func subsystemOf(layer string) string {
	switch layer {
	case "cmd":
		return subsystemCLI
	case "root":
		return subsystemRoot
	}
	name := strings.TrimPrefix(layer, "internal/")
	switch name {
	case "index", "analyze", "gitchurn", "graphmetrics", "lifecycle":
		return subsystemIndexing
	case "store", "vector", "kg":
		return subsystemPersistence
	case "query", "search":
		return subsystemQuery
	case "diagram", "output", "c4":
		return subsystemPresentation
	default:
		return subsystemSupport
	}
}

// hasHighRank reports whether any package in a subsystem ranks in the top 3
// by PageRank (rank == 0 means "no ranking data" — never highlight on that).
func hasHighRank(pkgs []string, ranking map[string]int) bool {
	for _, p := range pkgs {
		if r, ok := ranking[p]; ok && r >= 1 && r <= 3 {
			return true
		}
	}
	return false
}

func renderSystemMapSummary(groups []systemGroup, groupEdges []groupEdge, pkgs []string, edges [][2]string, topN int, ranking map[string]int, module string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "diagram system-map · %d packages collapsed into %d subsystems · %d cross-subsystem relationships (was %d package edges)",
		len(pkgs), len(groups), len(groupEdges), len(edges))
	if topN > 0 && len(ranking) > 0 {
		fmt.Fprintf(&sb, " · trimmed to top-%d packages by PageRank", topN)
	}
	if module != "" {
		fmt.Fprintf(&sb, " · module=%s", module)
	}
	sb.WriteString("\n\n")

	for _, gr := range groups {
		fmt.Fprintf(&sb, "%s (%d package%s)\n", gr.name, len(gr.pkgs), plural(len(gr.pkgs)))
		for _, p := range gr.pkgs {
			fmt.Fprintf(&sb, "  %s\n", shortPkg(p, module))
		}
	}

	if len(groupEdges) > 0 {
		sb.WriteString("\nrelationships:\n")
		for _, e := range groupEdges {
			fmt.Fprintf(&sb, "  %s -> %s (%d)\n", e.from, e.to, e.count)
		}
	}

	return sb.String()
}

// systemNodeID derives a D2-safe node ID from a subsystem display name.
// diagram.SanitizeID strips spaces/dots/quotes but not "&", which is a D2
// operator character — left in place it produces an invalid unquoted
// identifier (e.g. "Indexing_&_Analysis"). Subsystem names are short,
// human-authored labels (not file paths), so replacing "&" with "and" before
// sanitizing keeps IDs valid without touching the shared sanitizer.
func systemNodeID(name string) string {
	return diagram.SanitizeID(strings.ReplaceAll(name, "&", "and"))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
