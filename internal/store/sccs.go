package store

import "fmt"

// SCCRow is a single (node, scc_id, scc_size) row from graph_sccs.
type SCCRow struct {
	NodeID  string
	SCCID   int
	SCCSize int
}

// WriteSCCs replaces all rows for graphKind in graph_sccs with one row per node
// across the supplied nontrivial SCCs. Each component is assigned a sequential
// scc_id starting at 1 in the order provided. The whole replacement runs in
// a single transaction.
func (s *Store) WriteSCCs(graphKind string, sccs [][]string) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { rollbackOnError(tx, &err) }()

	if _, err := tx.Exec(
		`DELETE FROM graph_sccs WHERE graph_kind = ?`,
		graphKind,
	); err != nil {
		return fmt.Errorf("delete existing sccs: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO graph_sccs (graph_kind, node_id, scc_id, scc_size) VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i, comp := range sccs {
		sccID := i + 1
		size := len(comp)
		for _, node := range comp {
			if _, err := stmt.Exec(graphKind, node, sccID, size); err != nil {
				return fmt.Errorf("insert scc row: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// ReadSCCs returns all rows for graphKind ordered by scc_id ASC, node_id ASC.
func (s *Store) ReadSCCs(graphKind string) ([]SCCRow, error) {
	rs, err := s.db.Query(
		`SELECT node_id, scc_id, scc_size FROM graph_sccs
		 WHERE graph_kind = ? ORDER BY scc_id ASC, node_id ASC`,
		graphKind,
	)
	if err != nil {
		return nil, fmt.Errorf("query graph_sccs: %w", err)
	}
	defer func() { _ = rs.Close() }()

	var out []SCCRow
	for rs.Next() {
		var r SCCRow
		if err := rs.Scan(&r.NodeID, &r.SCCID, &r.SCCSize); err != nil {
			return nil, fmt.Errorf("scan scc row: %w", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate scc rows: %w", err)
	}
	return out, nil
}
