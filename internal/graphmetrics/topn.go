package graphmetrics

import (
	"fmt"

	"github.com/dkoosis/snipe/internal/store"
)

// TopNByPageRank returns the set of nodes to retain and a node->rank map for
// the requested graph kind ("imports" or "calls").
//
// Behavior:
//   - topN <= 0: returns (nil, nil, nil) meaning "no trim — keep everything".
//   - graph_metrics empty for (graphKind, "pagerank"): returns (nil, nil, nil)
//     so callers fall back to the un-trimmed graph rather than erroring.
//   - otherwise: returns a keep-set of size <= topN and a parallel rank map.
//
// Rank values are 1-based (smaller = more central).
func TopNByPageRank(s *store.Store, graphKind string, topN int) (map[string]bool, map[string]int, error) {
	if topN <= 0 {
		return nil, nil, nil
	}
	rows, err := s.ReadTopN(graphKind, "pagerank", topN)
	if err != nil {
		return nil, nil, fmt.Errorf("read pagerank: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	keep := make(map[string]bool, len(rows))
	rank := make(map[string]int, len(rows))
	for _, r := range rows {
		keep[r.NodeID] = true
		rank[r.NodeID] = r.Rank
	}
	return keep, rank, nil
}
