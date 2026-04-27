package graphmetrics

// TopoSort returns a Kahn topological order of g's nodes. If g has a cycle,
// order is nil and cycleWitness contains nodes from one cycle in DFS-traversal
// order (not necessarily minimal).
func TopoSort(g *Graph) (order []string, cycleWitness []string) {
	nodes := g.Nodes()
	indeg := InDegree(g)
	queue := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	order = make([]string, 0, len(nodes))
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		order = append(order, u)
		for _, v := range g.Adj[u] {
			indeg[v]--
			if indeg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}
	if len(order) == len(nodes) {
		return order, nil
	}
	// Cycle exists. Find a witness via DFS from any remaining node.
	remaining := make(map[string]bool, len(nodes)-len(order))
	for _, n := range nodes {
		remaining[n] = true
	}
	for _, n := range order {
		delete(remaining, n)
	}
	var start string
	for _, n := range nodes {
		if remaining[n] {
			start = n
			break
		}
	}
	onStack := make(map[string]int) // node -> index in path
	path := []string{}
	var dfs func(u string) []string
	dfs = func(u string) []string {
		if idx, ok := onStack[u]; ok {
			return append([]string{}, path[idx:]...)
		}
		if !remaining[u] {
			return nil
		}
		onStack[u] = len(path)
		path = append(path, u)
		for _, v := range g.Adj[u] {
			if w := dfs(v); w != nil {
				return w
			}
		}
		path = path[:len(path)-1]
		delete(onStack, u)
		remaining[u] = false
		return nil
	}
	if w := dfs(start); w != nil {
		return nil, w
	}
	// Shouldn't reach here, but return remaining as a fallback witness.
	w := make([]string, 0, len(remaining))
	for n := range remaining {
		w = append(w, n)
	}
	return nil, w
}
