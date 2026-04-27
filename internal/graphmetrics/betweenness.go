package graphmetrics

// Betweenness computes Brandes (2001) unweighted betweenness centrality on the
// directed graph g. Returns a map of node -> score, normalized by (N-1)(N-2).
//
// For directed graphs Brandes (§3) accumulates each pair-dependency once
// (no division by 2). Normalization divides by (N-1)(N-2), the maximum
// possible number of shortest-path pairs through a non-endpoint node, so
// scores fall in [0, 1].
//
// Returns an empty map for graphs with fewer than 3 nodes (no internal
// pair-paths exist; normalization is undefined).
func Betweenness(g *Graph) map[string]float64 {
	nodes := g.Nodes()
	n := len(nodes)
	cb := make(map[string]float64, n)
	for _, v := range nodes {
		cb[v] = 0
	}
	if n < 3 {
		return cb
	}

	for _, s := range nodes {
		// Single-source shortest paths (BFS) — Brandes Algorithm 1.
		stack := make([]string, 0, n)
		pred := make(map[string][]string, n)
		sigma := make(map[string]float64, n)
		dist := make(map[string]int, n)
		for _, t := range nodes {
			sigma[t] = 0
			dist[t] = -1
		}
		sigma[s] = 1
		dist[s] = 0

		queue := []string{s}
		for len(queue) > 0 {
			v := queue[0]
			queue = queue[1:]
			stack = append(stack, v)
			for _, w := range g.OutEdges(v) {
				if dist[w] < 0 {
					dist[w] = dist[v] + 1
					queue = append(queue, w)
				}
				if dist[w] == dist[v]+1 {
					sigma[w] += sigma[v]
					pred[w] = append(pred[w], v)
				}
			}
		}

		// Accumulation — reverse BFS order.
		delta := make(map[string]float64, n)
		for i := len(stack) - 1; i >= 0; i-- {
			w := stack[i]
			for _, v := range pred[w] {
				delta[v] += (sigma[v] / sigma[w]) * (1.0 + delta[w])
			}
			if w != s {
				cb[w] += delta[w]
			}
		}
	}

	// Normalize by (N-1)(N-2) for directed graphs.
	norm := float64((n - 1) * (n - 2))
	for k := range cb {
		cb[k] /= norm
	}
	return cb
}
