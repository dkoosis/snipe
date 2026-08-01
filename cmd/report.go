package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

// report.go — `snipe report`: a self-contained HTML dashboard for HUMANS,
// aggregating snipe's own metrics (sn-qnq3, spun off sn-fhw8's D1 amendment /
// D7 in CLAUDE.md). Distinct from sn-l1kh's per-command docs/diagrams/*.md
// living docs: this is snipe's one deliberate multi-artifact human page.
//
// v1 layout: native visuals snipe renders directly from indexed data —
// hotspot treemap (report_treemap.go), cycles, hotspots ranking table — plus
// placeholder slots for D2 diagrams (arch/flow/lifecycle) that an EXTERNAL
// script fills later by running `snipe diagram X | d2 -` and injecting the
// SVG. Snipe does NOT shell out to `d2` itself here — that keeps the `d2` CLI
// dependency out of snipe's own report path, per the ratified design (D7).

var (
	reportOut       string
	reportEmitShell bool
	reportTopN      int
)

// ReportCmd is the `snipe report` CLI command. Registered in cmd/cli.go next
// to Diagram/Lifecycle/C4 (kept in its own file rather than commands.go,
// following the C4Cmd precedent in cmd/c4.go).
type ReportCmd struct {
	Out       string `default:".snipe/report" help:"Output directory for report.html + manifest.json"`
	EmitShell bool   `name:"emit-shell" default:"true" help:"Emit the HTML shell (native visuals + D2 placeholder slots) and manifest.json — v1's only supported mode"`
	Top       int    `default:"30" help:"Top-N files in the hotspot treemap and ranking table"`
}

// Run implements kong's command interface.
func (c *ReportCmd) Run() error {
	reportOut = c.Out
	reportEmitShell = c.EmitShell
	reportTopN = c.Top
	return runReport()
}

// reportSummary is the --format json result for `snipe report`.
type reportSummary struct {
	HTMLPath     string `json:"html_path"`
	ManifestPath string `json:"manifest_path"`
	Files        int    `json:"files"`
	Slots        int    `json:"slots"`
}

// runReport assembles the dashboard and writes report.html + manifest.json
// under --out. Inputs are already indexed (hotspot rows, graph_sccs,
// per-file func counts) — this is a rendering/assembly layer, no new
// analysis.
func runReport() error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	if !reportEmitShell {
		return w.WriteError(cmdNameReport, &output.Error{
			Code:    output.ErrInternal,
			Message: "v1 only supports --emit-shell (native visuals + D2 manifest); no other mode exists yet",
		})
	}

	s, dir, err := OpenStore(w, cmdNameReport)
	if err != nil {
		return err
	}
	defer s.Close()

	rows, _, err := loadHotspotRows(s)
	if err != nil {
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Path < rows[j].Path
	})
	if reportTopN > 0 && len(rows) > reportTopN {
		rows = rows[:reportTopN]
	}

	funcCounts, err := fileFuncCounts(s)
	if err != nil {
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	items := make([]treemapItem, 0, len(rows))
	for _, r := range rows {
		size := float64(funcCounts[r.Path])
		if size <= 0 {
			// Keep the file visible even with zero recorded funcs (e.g. a
			// pure-const/type file) rather than collapsing it to nothing.
			size = 1
		}
		items = append(items, treemapItem{Path: r.Path, Size: size, Complexity: float64(r.Cyclo)})
	}
	boxes := layoutTreemap(items)

	importSCCs, err := s.ReadSCCs(cmdNameImports)
	if err != nil {
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	callSCCs, err := s.ReadSCCs("calls")
	if err != nil {
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}

	manifest := buildReportManifest(start)

	htmlDoc := assembleReportHTML(
		dir,
		start.UTC().Format(time.RFC3339),
		renderTreemapHTML(boxes),
		renderCyclesHTML(cmdNameImports, groupSCCs(importSCCs)),
		renderCyclesHTML("calls", groupSCCs(callSCCs)),
		renderHotspotsTableHTML(rows, funcCounts),
		renderDiagramSlotsHTML(manifest.Slots),
	)

	if err := os.MkdirAll(reportOut, 0o755); err != nil {
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: "mkdir out: " + err.Error()})
	}
	htmlPath := filepath.Join(reportOut, "report.html")
	if err := os.WriteFile(htmlPath, []byte(htmlDoc), 0o644); err != nil { //nolint:gosec // dashboard output, not sensitive
		return w.WriteError(cmdNameReport, &output.Error{Code: output.ErrInternal, Message: "write report.html: " + err.Error()})
	}
	manifestPath := filepath.Join(reportOut, "manifest.json")
	if err := writeJSONFile(manifestPath, manifest); err != nil {
		return err
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[reportSummary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results: []reportSummary{{
				HTMLPath:     htmlPath,
				ManifestPath: manifestPath,
				Files:        len(rows),
				Slots:        len(manifest.Slots),
			}},
			Meta: output.Meta{
				Command:  cmdNameReport,
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    1,
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	fmt.Printf("report · %s + %s · %d files · %d D2 slots pending external render\n",
		htmlPath, manifestPath, len(rows), len(manifest.Slots))
	return nil
}

// fileFuncCounts returns per-file function+method counts (production code
// only — _test.go excluded, matching computeFileComplexity's convention so
// treemap sizing and the complexity coloring agree on which files count as
// "hot"). Keyed by file_path_rel to match hotspotRow.Path's relative form.
func fileFuncCounts(s *store.Store) (map[string]int, error) {
	rows, err := s.DB().Query(`
		SELECT file_path_rel, COUNT(*)
		FROM symbols
		WHERE kind IN ('func', 'method')
		  AND file_path_rel IS NOT NULL
		  AND file_path_rel NOT LIKE '%_test.go'
		GROUP BY file_path_rel`)
	if err != nil {
		return nil, fmt.Errorf("query file func counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int)
	for rows.Next() {
		var path string
		var n int
		if err := rows.Scan(&path, &n); err != nil {
			return nil, fmt.Errorf("scan func count row: %w", err)
		}
		out[path] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate func count rows: %w", err)
	}
	return out, nil
}

// groupSCCs collapses raw graph_sccs rows into grouped components — mirrors
// runCyclesMetrics' grouping in metrics_cycles.go (kept as a small local copy
// rather than refactoring that file, so this change's footprint is new files
// only); sccGroup itself is shared, since both live in package cmd.
func groupSCCs(rows []store.SCCRow) []sccGroup {
	groupMap := make(map[int]*sccGroup)
	var order []int
	for _, r := range rows {
		g, ok := groupMap[r.SCCID]
		if !ok {
			g = &sccGroup{ID: r.SCCID, Size: r.SCCSize}
			groupMap[r.SCCID] = g
			order = append(order, r.SCCID)
		}
		g.Nodes = append(g.Nodes, r.NodeID)
	}
	groups := make([]sccGroup, 0, len(order))
	for _, id := range order {
		g := groupMap[id]
		if g.Size == 1 && len(g.Nodes) == 1 {
			g.SelfLoop = true
		}
		groups = append(groups, *g)
	}
	return groups
}
