package lifecycle

import (
	"database/sql"
	"strings"
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
//
// The walk processes one whole BFS frontier (every node at a given depth) per
// iteration with a single batched query, rather than one query per node. This
// collapses the former N-queries-per-walk into one-query-per-level. The query
// mirrors query.FindCallers' selection (test-file callers included, no role
// filtering), so results are unchanged from the per-node version.
func WalkCallers(db *sql.DB, symbolID string, maxDepth int) []CallerNode {
	if maxDepth <= 0 {
		return nil
	}
	visited := map[string]bool{symbolID: true}
	var result []CallerNode

	frontier := []string{symbolID}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		edges, err := frontierCallers(db, frontier)
		if err != nil {
			break
		}
		var next []string
		for i := range edges {
			e := &edges[i]
			if visited[e.callerID] {
				continue
			}
			visited[e.callerID] = true
			result = append(result, CallerNode{
				ID:    e.callerID,
				Name:  e.callerName,
				File:  e.callerFileRel,
				Depth: depth + 1,
			})
			next = append(next, e.callerID)
		}
		frontier = next
	}
	return result
}

// callerEdge is one caller→callee edge fetched during the BFS walk.
type callerEdge struct {
	callerID      string
	callerName    string
	callerFileRel string
}

// frontierCallers returns the direct callers of every callee ID in the frontier
// in a single query. It mirrors query.FindCallers' selection (test-file callers
// included, no role filtering) but fetches only the three columns the walk needs
// and batches the whole BFS frontier into one IN(...) query — replacing the
// former per-node round trips with one query per BFS level. DISTINCT collapses a
// caller reached via several frontier callees (or several call sites) to one row.
func frontierCallers(db *sql.DB, ids []string) ([]callerEdge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// SQLite's default SQLITE_LIMIT_VARIABLE_NUMBER is 999; a wide BFS frontier
	// (e.g. a heavily-called helper) can exceed it, so chunk the IN(...) set.
	const maxVars = 999
	var edges []callerEdge
	for start := 0; start < len(ids); start += maxVars {
		end := start + maxVars
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		if err := func() error {
			placeholders := make([]string, len(chunk))
			args := make([]any, len(chunk))
			for i, id := range chunk {
				placeholders[i] = "?"
				args[i] = id
			}
			// #nosec G201 -- placeholders are "?" literals; args carry the values (parameterized).
			q := `
				SELECT DISTINCT cg.caller_id, caller.name, caller.file_path_rel
				FROM call_graph cg
				JOIN symbols caller ON cg.caller_id = caller.id
				WHERE cg.callee_id IN (` + strings.Join(placeholders, ",") + `)
				ORDER BY caller.file_path_rel, caller.name`

			rows, err := db.Query(q, args...)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var e callerEdge
				if err := rows.Scan(&e.callerID, &e.callerName, &e.callerFileRel); err != nil {
					return err
				}
				edges = append(edges, e)
			}
			return rows.Err()
		}(); err != nil {
			return nil, err
		}
	}
	return edges, nil
}
