package graphmetrics

import "math"

// InDegree returns in-degree count per node (incoming edges).
func InDegree(g *Graph) map[string]float64 {
	out := make(map[string]float64, len(g.Adj))
	for _, n := range g.Nodes() {
		out[n] = 0
	}
	for _, dsts := range g.Adj {
		for _, d := range dsts {
			out[d]++
		}
	}
	return out
}

// OutDegree returns out-degree count per node (outgoing edges).
func OutDegree(g *Graph) map[string]float64 {
	out := make(map[string]float64, len(g.Adj))
	for _, n := range g.Nodes() {
		out[n] = float64(len(g.Adj[n]))
	}
	return out
}

// EigenvectorCentrality computes the principal eigenvector of the adjacency
// transpose via power iteration: a node's score grows when high-scoring nodes
// point at it. L2-normalized; converges in ~maxIter steps for small graphs.
func EigenvectorCentrality(g *Graph, maxIter int) map[string]float64 {
	nodes := g.Nodes()
	n := len(nodes)
	if n == 0 {
		return map[string]float64{}
	}
	const tol = 1e-8
	x := make(map[string]float64, n)
	next := make(map[string]float64, n)
	init := 1.0 / math.Sqrt(float64(n))
	for _, v := range nodes {
		x[v] = init
	}
	for iter := 0; iter < maxIter; iter++ {
		for _, v := range nodes {
			next[v] = 0
		}
		// next[dst] += x[src] for each edge src->dst (adjacency transpose).
		for src, dsts := range g.Adj {
			xs := x[src]
			for _, d := range dsts {
				next[d] += xs
			}
		}
		var norm float64
		for _, v := range nodes {
			norm += next[v] * next[v]
		}
		norm = math.Sqrt(norm)
		if norm == 0 {
			// All mass collapsed to sinks (next-vector is zero). The previous
			// iterate is the most informative answer we have; return it.
			return x
		}
		var delta float64
		for _, v := range nodes {
			nv := next[v] / norm
			delta += math.Abs(nv - x[v])
			x[v] = nv
		}
		if delta < tol {
			break
		}
	}
	return x
}
