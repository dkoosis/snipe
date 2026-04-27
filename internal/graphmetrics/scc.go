package graphmetrics

import "sort"

// SCC returns the nontrivial strongly-connected components of g, computed via
// Tarjan's 1972 algorithm. A component is nontrivial when it contains 2+ nodes,
// or a single node with a self-loop.
//
// Each returned component is a slice of node IDs sorted alphabetically. The
// outer slice is sorted by component size descending, then by the first
// (lowest-sorted) node ID for stability.
func SCC(g *Graph) [][]string {
	nodes := g.Nodes()
	if len(nodes) == 0 {
		return nil
	}

	t := &tarjan{
		g:       g,
		index:   make(map[string]int, len(nodes)),
		lowlink: make(map[string]int, len(nodes)),
		onStack: make(map[string]bool, len(nodes)),
	}

	for _, v := range nodes {
		if _, seen := t.index[v]; !seen {
			t.strongconnect(v)
		}
	}

	// Filter to nontrivial components.
	out := make([][]string, 0, len(t.components))
	for _, comp := range t.components {
		if len(comp) >= 2 {
			sort.Strings(comp)
			out = append(out, comp)
			continue
		}
		// Single-node component: keep only if there is a self-loop.
		v := comp[0]
		hasSelfLoop := false
		for _, w := range g.OutEdges(v) {
			if w == v {
				hasSelfLoop = true
				break
			}
		}
		if hasSelfLoop {
			out = append(out, comp)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// tarjan holds state for a single SCC computation.
type tarjan struct {
	g          *Graph
	counter    int
	stack      []string
	index      map[string]int
	lowlink    map[string]int
	onStack    map[string]bool
	components [][]string
}

func (t *tarjan) strongconnect(v string) {
	t.index[v] = t.counter
	t.lowlink[v] = t.counter
	t.counter++
	t.stack = append(t.stack, v)
	t.onStack[v] = true

	for _, w := range t.g.OutEdges(v) {
		if _, seen := t.index[w]; !seen {
			t.strongconnect(w)
			if t.lowlink[w] < t.lowlink[v] {
				t.lowlink[v] = t.lowlink[w]
			}
		} else if t.onStack[w] {
			if t.index[w] < t.lowlink[v] {
				t.lowlink[v] = t.index[w]
			}
		}
	}

	if t.lowlink[v] == t.index[v] {
		var comp []string
		for {
			n := len(t.stack) - 1
			w := t.stack[n]
			t.stack = t.stack[:n]
			t.onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		t.components = append(t.components, comp)
	}
}
