package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

// hotspotRow is one file ranked by the Tornhill hotspot model.
type hotspotRow struct {
	Path        string  `json:"path"`
	Score       float64 `json:"score"`        // 0..1, complexity × change-frequency
	Cyclo       int     `json:"cyclo"`        // summed McCabe complexity of the file's funcs
	CycloMax    int     `json:"cyclo_max"`    // hottest single function
	Commits     int     `json:"commits"`      // revisions touching the file
	Authors     int     `json:"authors"`      // distinct committers (bus factor)
	LastChanged string  `json:"last_changed"` // empty when churn unavailable
}

// runHotspots ranks files by complexity × change-frequency — Tornhill's
// single most practical risk heuristic (Your Code as a Crime Scene, 2015;
// CodeScene). Complex code that never changes is stable and churning simple
// code is cheap; risk concentrates in the overlap. It joins per-file McCabe
// complexity (computeFileCyclo) with git churn (file_churn). Outside a git
// repo it degrades to a complexity-only ranking with a printed note.
func runHotspots(top int, pkg, file string) error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, dir, err := OpenStore(w, cmdNameHotspots)
	if err != nil {
		return err
	}
	defer s.Close()
	start := time.Now()

	cycloSum, err := s.ReadTopN("files", "cyclo_sum", 0)
	if err != nil {
		return w.WriteError(cmdNameHotspots, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	if len(cycloSum) == 0 {
		_, werr := os.Stdout.WriteString("hotspots · (no complexity data — run `snipe index` to populate)\n")
		return werr
	}
	cycloMax, err := s.ReadTopN("files", "cyclo_max", 0)
	if err != nil {
		return w.WriteError(cmdNameHotspots, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	churn, err := s.ReadFileChurnTopN(0)
	if err != nil {
		return w.WriteError(cmdNameHotspots, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	maxByPath := make(map[string]int, len(cycloMax))
	for _, r := range cycloMax {
		maxByPath[r.NodeID] = int(r.Value)
	}
	churnByPath := make(map[string]store.FileChurn, len(churn))
	for _, c := range churn {
		churnByPath[c.Path] = c
	}
	haveChurn := len(churn) > 0

	rows := buildHotspots(cycloSum, maxByPath, churnByPath, haveChurn)
	rows = filterHotspots(rows, pkg, file)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Path < rows[j].Path
	})
	if top > 0 && len(rows) > top {
		rows = rows[:top]
	}

	if GetOutputFormat() == output.OutputJSON {
		return writeHotspotsJSON(rows, dir, start)
	}
	return writeHotspotsText(rows, haveChurn)
}

// buildHotspots joins complexity with churn and assigns a normalized score.
// Complexity is normalized linearly; churn is log-scaled first because commit
// counts are heavy-tailed (a few files dominate). With churn present, only
// files that have history are scored (inner join); without it, every file is
// scored on complexity alone so the command still ranks something useful.
func buildHotspots(cycloSum []store.MetricRow, maxByPath map[string]int, churnByPath map[string]store.FileChurn, haveChurn bool) []hotspotRow {
	var maxCyclo, maxLogChurn float64
	for _, r := range cycloSum {
		if r.Value > maxCyclo {
			maxCyclo = r.Value
		}
	}
	if haveChurn {
		for _, c := range churnByPath {
			if l := math.Log1p(float64(c.Commits)); l > maxLogChurn {
				maxLogChurn = l
			}
		}
	}

	rows := make([]hotspotRow, 0, len(cycloSum))
	for _, r := range cycloSum {
		c, ok := churnByPath[r.NodeID]
		if haveChurn && !ok {
			continue // no history → not a hotspot in the crime-scene sense
		}
		cycloNorm := 0.0
		if maxCyclo > 0 {
			cycloNorm = r.Value / maxCyclo
		}
		score := cycloNorm
		if haveChurn && maxLogChurn > 0 {
			score = cycloNorm * (math.Log1p(float64(c.Commits)) / maxLogChurn)
		}
		rows = append(rows, hotspotRow{
			Path:        r.NodeID,
			Score:       score,
			Cyclo:       int(r.Value),
			CycloMax:    maxByPath[r.NodeID],
			Commits:     c.Commits,
			Authors:     c.Authors,
			LastChanged: c.LastChanged,
		})
	}
	return rows
}

func filterHotspots(rows []hotspotRow, pkg, file string) []hotspotRow {
	if pkg == "" && file == "" {
		return rows
	}
	out := rows[:0]
	for _, r := range rows {
		if pkg != "" && !strings.Contains(r.Path, pkg) {
			continue
		}
		if file != "" && !strings.Contains(r.Path, file) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func writeHotspotsJSON(rows []hotspotRow, dir string, start time.Time) error {
	resp := output.Response[hotspotRow]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  rows,
		Meta: output.Meta{
			Command:  cmdNameHotspots,
			RepoRoot: dir,
			Ms:       time.Since(start).Milliseconds(),
			Total:    len(rows),
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func writeHotspotsText(rows []hotspotRow, haveChurn bool) error {
	var b strings.Builder
	b.WriteString("hotspots · complexity × change-frequency (Tornhill crime-scene: risk peaks where complex code changes often)\n")
	if !haveChurn {
		b.WriteString("  ⚠ no git churn available (not a git repo) — ranking by complexity only\n")
	}
	if len(rows) == 0 {
		b.WriteString("  (no rows)\n")
		_, err := os.Stdout.WriteString(b.String())
		return err
	}
	if haveChurn {
		fmt.Fprintf(&b, "  %5s %6s %5s %8s %8s  %-12s  %s\n", "score", "cyclo", "cmax", "commits", "authors", "last-changed", "path")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %5.2f %6d %5d %8d %8d  %-12s  %s\n",
				r.Score, r.Cyclo, r.CycloMax, r.Commits, r.Authors, r.LastChanged, r.Path)
		}
	} else {
		fmt.Fprintf(&b, "  %5s %6s %5s  %s\n", "score", "cyclo", "cmax", "path")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %5.2f %6d %5d  %s\n", r.Score, r.Cyclo, r.CycloMax, r.Path)
		}
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
