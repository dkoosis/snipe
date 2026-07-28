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
	"github.com/dkoosis/snipe/internal/sensitive"
	"github.com/dkoosis/snipe/internal/store"
)

// hotspotRow is one file in the unified risk view. Score is the grounded
// Tornhill hotspot (complexity × change-frequency); the other fields are the
// risk components Claude reads to judge the dominant factor — they are NOT
// folded into the score.
type hotspotRow struct {
	Path        string           `json:"path"`
	Score       float64          `json:"score"`               // 0..1, Tornhill hotspot = cyclo × churn
	Cyclo       int              `json:"cyclo"`               // summed McCabe complexity (cx)
	CycloMax    int              `json:"cyclo_max"`           // hottest single function
	Cognitive   int              `json:"cognitive"`           // summed SonarSource cognitive complexity (cog)
	Commits     int              `json:"commits"`             // revisions touching the file (chn)
	Authors     int              `json:"authors"`             // distinct committers / bus factor (auth)
	FanIn       int              `json:"fan_in"`              // distinct caller files / blast radius (in)
	LastChanged string           `json:"last_changed"`        // empty when churn unavailable
	Sensitive   []sensitive.Zone `json:"sensitive,omitempty"` // auth/crypto/migration/... if any
}

// loadHotspotRows reads the per-file complexity and churn metrics and joins
// them into unified risk rows, exactly as runHotspots does — factored out so
// other commands (e.g. `snipe triage`) can look up a file's hotspot row
// without duplicating the read+join. Returns (nil, false, nil) when the index
// has no complexity data yet (distinct from an empty-but-populated index).
func loadHotspotRows(s *store.Store) ([]hotspotRow, bool, error) {
	cycloSum, err := s.ReadTopN("files", "cyclo_sum", 0)
	if err != nil {
		return nil, false, err
	}
	if len(cycloSum) == 0 {
		return nil, false, nil
	}
	cycloMax, err := s.ReadTopN("files", "cyclo_max", 0)
	if err != nil {
		return nil, false, err
	}
	cognitiveSum, err := s.ReadTopN("files", "cognitive_sum", 0)
	if err != nil {
		return nil, false, err
	}
	fanInRows, err := s.ReadTopN("files", "fan_in", 0)
	if err != nil {
		return nil, false, err
	}
	churn, err := s.ReadFileChurnTopN(0)
	if err != nil {
		return nil, false, err
	}

	maxByPath := intByPath(cycloMax)
	cogByPath := intByPath(cognitiveSum)
	fanByPath := intByPath(fanInRows)
	churnByPath := make(map[string]store.FileChurn, len(churn))
	for _, c := range churn {
		churnByPath[c.Path] = c
	}
	haveChurn := len(churn) > 0

	rows := buildHotspots(cycloSum, maxByPath, cogByPath, fanByPath, churnByPath, haveChurn)
	return rows, haveChurn, nil
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

	rows, haveChurn, err := loadHotspotRows(s)
	if err != nil {
		return w.WriteError(cmdNameHotspots, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	if rows == nil {
		// Preserve the JSON envelope for API/orca consumers in the common
		// "index not populated yet" case; plain text is for humans only.
		if GetOutputFormat() == output.OutputJSON {
			return writeHotspotsJSON(nil, dir, start)
		}
		_, werr := os.Stdout.WriteString("hotspots · (no complexity data — run `snipe index` to populate)\n")
		return werr
	}

	rows = filterHotspots(rows, pkg, file)
	// Annotate with sensitive zones so a small-but-critical file (auth, crypto,
	// migration) is visible even when its complexity × churn score is modest.
	cls := sensitive.Load(dir)
	for i := range rows {
		rows[i].Sensitive = cls.Zones(rows[i].Path)
	}
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

// intByPath collapses metric rows into a path→int lookup.
func intByPath(rows []store.MetricRow) map[string]int {
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.NodeID] = int(r.Value)
	}
	return m
}

// buildHotspots assembles the unified risk rows. The score is the grounded
// Tornhill hotspot — norm(cyclo) × norm(log1p(commits)) — and nothing else;
// cognitive, fan-in and sensitivity ride along as context columns rather than
// being folded into an un-grounded composite. Complexity is normalized
// linearly; churn is log-scaled first because commit counts are heavy-tailed.
// With churn present, only files with history are scored (inner join); without
// it, every file is scored on complexity alone so the command still ranks.
func buildHotspots(cycloSum []store.MetricRow, maxByPath, cogByPath, fanByPath map[string]int, churnByPath map[string]store.FileChurn, haveChurn bool) []hotspotRow {
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
			Cognitive:   cogByPath[r.NodeID],
			Commits:     c.Commits,
			Authors:     c.Authors,
			FanIn:       fanByPath[r.NodeID],
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

// zoneSuffix renders a sensitive-zone marker appended to a hotspot's path,
// e.g. " ⚑[auth,crypto]", or "" when the file is in no sensitive zone.
func zoneSuffix(zones []sensitive.Zone) string {
	if len(zones) == 0 {
		return ""
	}
	return "  ⚑" + zonesString(zones)
}

func writeHotspotsText(rows []hotspotRow, haveChurn bool) error {
	var b strings.Builder
	b.WriteString("hotspots · unified risk (Tornhill crime-scene)\n")
	b.WriteString("  risk = cyclo × change-frequency; cx=cyclo cxmax=worst-func cyclo cog=cognitive chn=commits auth=bus-factor in=fan-in/blast-radius ⚑=sensitive\n")
	b.WriteString("  high cx + low cxmax → broad file, split; high cxmax → one gnarly func, simplify\n")
	if !haveChurn {
		b.WriteString("  ⚠ no git churn (not a git repo) — risk ranked on complexity alone; chn/auth omitted\n")
	}
	if len(rows) == 0 {
		b.WriteString("  (no rows)\n")
		_, err := os.Stdout.WriteString(b.String())
		return err
	}
	if haveChurn {
		fmt.Fprintf(&b, "  %5s %5s %5s %5s %5s %5s %5s  %s\n", "risk", "cx", "cxmax", "cog", "chn", "auth", "in", "path")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %5.2f %5d %5d %5d %5d %5d %5d  %s%s\n",
				r.Score, r.Cyclo, r.CycloMax, r.Cognitive, r.Commits, r.Authors, r.FanIn, r.Path, zoneSuffix(r.Sensitive))
		}
	} else {
		fmt.Fprintf(&b, "  %5s %5s %5s %5s %5s  %s\n", "risk", "cx", "cxmax", "cog", "in", "path")
		for _, r := range rows {
			fmt.Fprintf(&b, "  %5.2f %5d %5d %5d %5d  %s%s\n", r.Score, r.Cyclo, r.CycloMax, r.Cognitive, r.FanIn, r.Path, zoneSuffix(r.Sensitive))
		}
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
