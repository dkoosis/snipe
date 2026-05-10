package lifecycle

import (
	"database/sql"

	"github.com/dkoosis/snipe/internal/query"
)

// CallerNode is one hop in the caller chain for a lifecycle function.
type CallerNode struct {
	ID    string
	Name  string
	File  string
	Depth int
}

// WalkCallers returns all functions reachable by walking the caller graph
// upward from symbolID, up to maxDepth hops. BFS with a visited set prevents
// infinite recursion on mutually recursive or diamond call graphs.
// Returns nil when maxDepth <= 0.
func WalkCallers(db *sql.DB, symbolID string, maxDepth int) []CallerNode {
	if maxDepth <= 0 {
		return nil
	}
	visited := map[string]bool{symbolID: true}
	var result []CallerNode

	type item struct {
		id    string
		depth int
	}
	queue := []item{{symbolID, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxDepth {
			continue
		}
		rows, err := query.FindCallers(db, cur.id, 100, 0)
		if err != nil {
			continue
		}
		for i := range rows {
			r := &rows[i]
			if visited[r.CallerID] {
				continue
			}
			visited[r.CallerID] = true
			result = append(result, CallerNode{
				ID:    r.CallerID,
				Name:  r.CallerName,
				File:  r.CallerFileRel,
				Depth: cur.depth + 1,
			})
			queue = append(queue, item{r.CallerID, cur.depth + 1})
		}
	}
	return result
}
