// Package graphmetrics computes graph centrality metrics over the symbol graph.
package graphmetrics

import "math"

// PageRank computes PageRank scores for all nodes in g using the Brin & Page
// formulation ("The Anatomy of a Large-Scale Hypertextual Web Search Engine",
// WWW7 1998) with dangling-node mass redistribution. Damping is typically 0.85.
// Iteration stops when the L1 delta falls below 1e-6 or maxIter is reached.
// Returned scores sum to ~1.0.
func PageRank(g *Graph, damping float64, maxIter int) map[string]float64 {
	nodes := g.Nodes()
	n := len(nodes)
	if n == 0 {
		return map[string]float64{}
	}

	const tol = 1e-6
	nf := float64(n)
	pr := make(map[string]float64, n)
	for _, v := range nodes {
		pr[v] = 1.0 / nf
	}

	teleport := (1.0 - damping) / nf
	next := make(map[string]float64, n)

	for iter := 0; iter < maxIter; iter++ {
		// Sum mass held by dangling nodes (outdegree 0).
		var dangling float64
		for _, v := range nodes {
			if len(g.OutEdges(v)) == 0 {
				dangling += pr[v]
			}
		}
		danglingShare := damping * dangling / nf

		for _, v := range nodes {
			var inMass float64
			for _, u := range g.InEdges(v) {
				out := len(g.OutEdges(u))
				if out > 0 {
					inMass += pr[u] / float64(out)
				}
			}
			next[v] = teleport + danglingShare + damping*inMass
		}

		var delta float64
		for _, v := range nodes {
			delta += math.Abs(next[v] - pr[v])
			pr[v] = next[v]
		}
		if delta < tol {
			break
		}
	}
	return pr
}
