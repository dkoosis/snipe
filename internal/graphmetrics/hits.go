package graphmetrics

import "math"

// HITS computes Kleinberg hub and authority scores via power iteration.
//
// Mutual reinforcement:
//
//	auth[v] = sum over inlinks u of hub[u]
//	hub[v]  = sum over outlinks w of auth[w]
//
// Each iteration both vectors are L2-normalized (sum of squares = 1).
// Iteration stops on L1 delta < 1e-8 or after maxIter, whichever comes first.
//
// Read-path note: cmd/metrics.go accepts --kind=hits as a synonym for
// historical reasons, but only `hub` and `authority` are persisted. Querying
// --kind=hits returns no rows; use --kind=hub or --kind=authority instead.
func HITS(g *Graph, maxIter int) (hubs, authorities map[string]float64) {
	nodes := g.Nodes()
	n := len(nodes)
	hubs = make(map[string]float64, n)
	authorities = make(map[string]float64, n)
	if n == 0 {
		return hubs, authorities
	}
	init := 1.0 / math.Sqrt(float64(n))
	for _, v := range nodes {
		hubs[v] = init
		authorities[v] = init
	}

	const tol = 1e-8
	nextAuth := make(map[string]float64, n)
	nextHub := make(map[string]float64, n)

	for iter := 0; iter < maxIter; iter++ {
		// Authority update: auth[v] = sum of hub[u] for u -> v.
		var asum float64
		for _, v := range nodes {
			var s float64
			for _, u := range g.InEdges(v) {
				s += hubs[u]
			}
			nextAuth[v] = s
			asum += s * s
		}
		anorm := math.Sqrt(asum)
		if anorm == 0 {
			anorm = 1
		}
		for _, v := range nodes {
			nextAuth[v] /= anorm
		}

		// Hub update: hub[v] = sum of auth[w] for v -> w (use new authorities).
		var hsum float64
		for _, v := range nodes {
			var s float64
			for _, w := range g.OutEdges(v) {
				s += nextAuth[w]
			}
			nextHub[v] = s
			hsum += s * s
		}
		hnorm := math.Sqrt(hsum)
		if hnorm == 0 {
			hnorm = 1
		}
		var delta float64
		for _, v := range nodes {
			nextHub[v] /= hnorm
			delta += math.Abs(nextHub[v]-hubs[v]) + math.Abs(nextAuth[v]-authorities[v])
			hubs[v] = nextHub[v]
			authorities[v] = nextAuth[v]
		}
		if delta < tol {
			break
		}
	}
	return hubs, authorities
}
