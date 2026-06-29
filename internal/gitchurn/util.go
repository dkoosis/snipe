package gitchurn

import (
	"math"
	"sort"
)

// pow2 returns 2**x (x may be negative/fractional) — the recency decay term.
func pow2(x float64) float64 { return math.Exp2(x) }

// sortByCommits orders churn rows by commit count descending, breaking ties
// by path ascending so output is deterministic.
func sortByCommits(rows []FileChurn) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Commits != rows[j].Commits {
			return rows[i].Commits > rows[j].Commits
		}
		return rows[i].Path < rows[j].Path
	})
}
