package store

import (
	"database/sql"
	"fmt"
	"sort"
)

// MetricRow is a single (node, value, rank) row from graph_metrics.
type MetricRow struct {
	NodeID string
	Value  float64
	Rank   int
}

// WriteGraphMetrics replaces all rows for (graphKind, metric) with values from the map.
// Rows are sorted by value desc, then by node_id asc for stable tie-breaking, and assigned
// rank=1..N. The whole replacement happens in a single transaction.
func (s *Store) WriteGraphMetrics(graphKind, metric string, values map[string]float64) (err error) {
	type kv struct {
		node string
		val  float64
	}
	rows := make([]kv, 0, len(values))
	for n, v := range values {
		rows = append(rows, kv{node: n, val: v})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].val != rows[j].val {
			return rows[i].val > rows[j].val
		}
		return rows[i].node < rows[j].node
	})

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { rollbackOnError(tx, &err) }()

	if _, err := tx.Exec(
		`DELETE FROM graph_metrics WHERE graph_kind = ? AND metric = ?`,
		graphKind, metric,
	); err != nil {
		return fmt.Errorf("delete existing metrics: %w", err)
	}

	stmt, err := tx.Prepare(
		`INSERT INTO graph_metrics (graph_kind, node_id, metric, value, rank) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for i, r := range rows {
		if _, err := stmt.Exec(graphKind, r.node, metric, r.val, i+1); err != nil {
			return fmt.Errorf("insert metric row: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// FileFanIn returns, per file, the number of distinct caller symbols in other
// files that call into it — a blast-radius proxy for `snipe hotspots`: how
// much elsewhere depends on this file. Same-file calls are excluded (not
// external risk) and callers are counted distinctly so repeated calls from one
// function don't inflate the number. Keys come from symbols.file_path as
// stored (absolute); the caller relativizes to match the cyclo/churn keys.
func (s *Store) FileFanIn() (map[string]float64, error) {
	rows, err := s.db.Query(`
		SELECT callee.file_path AS f, COUNT(DISTINCT cg.caller_id) AS n
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE caller.file_path <> callee.file_path
		GROUP BY callee.file_path`)
	if err != nil {
		return nil, fmt.Errorf("query file fan-in: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]float64)
	for rows.Next() {
		var f string
		var n float64
		if err := rows.Scan(&f, &n); err != nil {
			return nil, fmt.Errorf("scan fan-in row: %w", err)
		}
		out[f] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fan-in rows: %w", err)
	}
	return out, nil
}

// ReadTopN returns the top-n rows for (graphKind, metric) ordered by rank ascending.
// If n <= 0, all rows are returned.
func (s *Store) ReadTopN(graphKind, metric string, n int) ([]MetricRow, error) {
	var (
		rs  *sql.Rows
		err error
	)
	if n <= 0 {
		rs, err = s.db.Query(
			`SELECT node_id, value, rank FROM graph_metrics
			 WHERE graph_kind = ? AND metric = ? ORDER BY rank ASC`,
			graphKind, metric,
		)
	} else {
		rs, err = s.db.Query(
			`SELECT node_id, value, rank FROM graph_metrics
			 WHERE graph_kind = ? AND metric = ? ORDER BY rank ASC LIMIT ?`,
			graphKind, metric, n,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query graph_metrics: %w", err)
	}
	defer func() { _ = rs.Close() }()

	var out []MetricRow
	for rs.Next() {
		var r MetricRow
		if err := rs.Scan(&r.NodeID, &r.Value, &r.Rank); err != nil {
			return nil, fmt.Errorf("scan metric row: %w", err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("iterate metric rows: %w", err)
	}
	return out, nil
}
