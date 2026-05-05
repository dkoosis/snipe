package graphmetrics

// Coupling holds Martin's package coupling metrics for a single node.
type Coupling struct {
	Ca int // afferent: count of nodes that depend on this one (incoming edges)
	Ce int // efferent: count of nodes this one depends on (outgoing edges)
}

// I returns Martin's instability metric: Ce / (Ca + Ce). Range [0, 1].
// 0 = maximally stable (only depended on); 1 = maximally unstable (only depends on others).
// Returns 0 for isolated nodes (Ca = Ce = 0) — convention: no coupling, no instability.
func (c Coupling) I() float64 {
	if c.Ca+c.Ce == 0 {
		return 0
	}
	return float64(c.Ce) / float64(c.Ca+c.Ce)
}

// ComputeCoupling returns Ca and Ce for every node in the graph.
// Edges represent "depends on" (importer -> imported), so:
//   - Ca(n) = number of nodes with edges into n
//   - Ce(n) = number of nodes n has edges out to
func ComputeCoupling(g *Graph) map[string]Coupling {
	nodes := g.Nodes()
	out := make(map[string]Coupling, len(nodes))
	for _, n := range nodes {
		out[n] = Coupling{Ce: len(g.Adj[n])}
	}
	for _, dsts := range g.Adj {
		for _, d := range dsts {
			c := out[d]
			c.Ca++
			out[d] = c
		}
	}
	return out
}
