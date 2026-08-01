package cmd

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
)

// report_html.go renders `snipe report`'s HTML shell: native visuals (treemap,
// cycles, hotspots table) inline, plus labeled placeholder <div>s for the D2
// diagram slots an external script fills in later (report_manifest.go lists
// the same slots as JSON). No templating library and no JS/CSS framework —
// D4 (token/dependency budget) applies to shipped artifacts too, and a static
// dashboard for humans doesn't need one.

// renderTreemapBoxHTML renders one treemap box as an absolutely positioned
// div inside a 100%x100% relatively-positioned container (the caller wraps
// the concatenated boxes in that container).
func renderTreemapBoxHTML(b treemapBox, maxComplexity float64) string {
	norm := 0.0
	if maxComplexity > 0 {
		norm = b.Complexity / maxComplexity
	}
	color := colorForComplexity(norm)
	label := filepath.Base(b.Path)
	// Skip the label on slivers too small to hold readable text — the title
	// attribute (tooltip) still carries the full path either way.
	showLabel := b.WPct > 6 && b.HPct > 4
	inner := ""
	if showLabel {
		inner = fmt.Sprintf(`<span class="tm-label">%s</span>`, html.EscapeString(label))
	}
	title := fmt.Sprintf("%s — complexity %.0f, size %.0f", b.Path, b.Complexity, b.Size)
	return fmt.Sprintf(
		`<div class="tm-box" style="left:%.3f%%;top:%.3f%%;width:%.3f%%;height:%.3f%%;background:%s" title='%s'>%s</div>`,
		b.XPct, b.YPct, b.WPct, b.HPct, color, html.EscapeString(title), inner,
	)
}

// renderTreemapHTML renders the full hotspot treemap: a relatively-positioned
// container holding one absolutely-positioned div per box. Empty input
// renders a "no data" placeholder rather than an empty container, so the
// shell is still valid/legible before the index has complexity data.
func renderTreemapHTML(boxes []treemapBox) string {
	if len(boxes) == 0 {
		return `<div class="empty-note">(no complexity data — run &#96;snipe index&#96; to populate)</div>`
	}
	maxComplexity := 0.0
	for _, b := range boxes {
		if b.Complexity > maxComplexity {
			maxComplexity = b.Complexity
		}
	}
	var b strings.Builder
	b.WriteString(`<div class="treemap">`)
	for _, box := range boxes {
		b.WriteString(renderTreemapBoxHTML(box, maxComplexity))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// renderCyclesHTML renders nontrivial SCCs for one graph kind ("imports" or
// "calls") as a labeled list — deliberately plain (no graph drawing): a cycle
// witness is a short list of node names, not worth a diagram slot.
func renderCyclesHTML(graphKind string, groups []sccGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="cycles-group"><h3>%s graph</h3>`, html.EscapeString(graphKind))
	if len(groups) == 0 {
		b.WriteString(`<div class="empty-note">no nontrivial cycles</div>`)
	} else {
		b.WriteString(`<ol class="cycles-list">`)
		for _, g := range groups {
			suffix := ""
			if g.SelfLoop {
				suffix = " (self-loop)"
			}
			fmt.Fprintf(&b, `<li><span class="cycle-size">size %d%s</span><ul>`, g.Size, suffix)
			for _, n := range g.Nodes {
				fmt.Fprintf(&b, `<li>%s</li>`, html.EscapeString(n))
			}
			b.WriteString(`</ul></li>`)
		}
		b.WriteString(`</ol>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// renderHotspotsTableHTML renders the unified risk rows (same data as `snipe
// hotspots`) as an HTML table — the ranking companion to the treemap's
// spatial view of the same complexity×churn scores.
func renderHotspotsTableHTML(rows []hotspotRow, funcCounts map[string]int) string {
	var b strings.Builder
	b.WriteString(`<table class="hotspots-table"><thead><tr>` +
		`<th>path</th><th>risk</th><th>cyclo</th><th>cyclo max</th><th>funcs</th><th>commits</th><th>authors</th><th>fan-in</th>` +
		`</tr></thead><tbody>`)
	if len(rows) == 0 {
		b.WriteString(`<tr><td colspan="8" class="empty-note">(no complexity data — run &#96;snipe index&#96; to populate)</td></tr>`)
	}
	for _, r := range rows {
		fmt.Fprintf(&b,
			`<tr><td>%s</td><td>%.2f</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>`,
			html.EscapeString(r.Path), r.Score, r.Cyclo, r.CycloMax, funcCounts[r.Path], r.Commits, r.Authors, r.FanIn)
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

// renderDiagramSlotsHTML renders one labeled placeholder <div> per D2 slot in
// `slots`. Each div's id matches the manifest slot's "id" exactly (both build
// off reportDiagramSlots) — that's the contract the external filler script
// relies on to know which SVG goes where.
func renderDiagramSlotsHTML(slots []reportManifestSlot) string {
	var b strings.Builder
	b.WriteString(`<div class="diagram-slots">`)
	for _, s := range slots {
		fmt.Fprintf(&b,
			`<div class="diagram-slot" id="%s" data-artifact-type="%s">`+
				`<div class="slot-label">%s</div>`+
				`<div class="slot-placeholder">(pending: %s)</div>`+
				`</div>`,
			html.EscapeString(s.ID), html.EscapeString(s.ArtifactType),
			html.EscapeString(s.Description), html.EscapeString(s.Command))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// reportCSS is the dashboard's entire stylesheet, inlined so report.html is a
// single self-contained file — no external CSS/JS request, matches the
// artifact-hygiene bar this codebase already holds Claude-facing output to.
const reportCSS = `
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#666;--border:#ddd;--card:#f7f7f8}
@media (prefers-color-scheme:dark){:root{--bg:#0f1115;--fg:#e6e6e6;--muted:#9aa0a6;--border:#2a2d33;--card:#181a1f}}
*{box-sizing:border-box}
body{margin:0;padding:2rem;background:var(--bg);color:var(--fg);font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
h1{font-size:1.4rem;margin:0 0 .25rem}
h2{font-size:1.1rem;margin:2rem 0 .75rem;border-bottom:1px solid var(--border);padding-bottom:.35rem}
h3{font-size:.95rem;margin:0 0 .5rem;color:var(--muted)}
.subtitle{color:var(--muted);margin:0 0 1rem;font-size:.9rem}
section{margin-bottom:2rem}
.treemap{position:relative;width:100%;height:420px;border:1px solid var(--border);border-radius:6px;overflow:hidden}
.tm-box{position:absolute;border:1px solid rgba(0,0,0,.15);overflow:hidden;display:flex;align-items:flex-end;padding:2px 4px}
.tm-label{font-size:.7rem;color:#111;background:rgba(255,255,255,.75);padding:0 3px;border-radius:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:100%}
.cycles{display:flex;gap:2rem;flex-wrap:wrap}
.cycles-group{background:var(--card);border:1px solid var(--border);border-radius:6px;padding:1rem;min-width:260px;flex:1}
.cycles-list{margin:0;padding-left:1.2rem}
.cycle-size{font-weight:600}
.empty-note{color:var(--muted);font-style:italic;padding:.5rem 0}
table.hotspots-table{border-collapse:collapse;width:100%;font-size:.85rem}
table.hotspots-table th,table.hotspots-table td{border-bottom:1px solid var(--border);padding:.4rem .6rem;text-align:right}
table.hotspots-table th:first-child,table.hotspots-table td:first-child{text-align:left;font-family:ui-monospace,monospace}
table.hotspots-table thead th{color:var(--muted);font-weight:600}
.diagram-slots{display:flex;gap:1rem;flex-wrap:wrap}
.diagram-slot{background:var(--card);border:1px dashed var(--border);border-radius:6px;padding:1rem;min-width:220px;flex:1}
.slot-label{font-weight:600;margin-bottom:.4rem}
.slot-placeholder{color:var(--muted);font-size:.8rem;font-family:ui-monospace,monospace}
footer{margin-top:3rem;color:var(--muted);font-size:.8rem}
`

// assembleReportHTML wraps every rendered section into one self-contained
// HTML document. generatedAt/repoRoot are for the footer; the rest are the
// pre-rendered fragments from the render* functions above.
func assembleReportHTML(repoRoot, generatedAt, treemapHTML, importCyclesHTML, callCyclesHTML, hotspotsTableHTML, diagramSlotsHTML string) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html><head><meta charset=\"utf-8\">")
	b.WriteString(`<meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString("<title>snipe report</title><style>")
	b.WriteString(reportCSS)
	b.WriteString("</style></head><body>")

	fmt.Fprintf(&b, `<h1>snipe report</h1><p class="subtitle">%s &middot; generated %s</p>`,
		html.EscapeString(repoRoot), html.EscapeString(generatedAt))

	b.WriteString(`<section><h2>hotspot treemap</h2><p class="subtitle">size = function count &middot; color = complexity (green low &rarr; red high)</p>`)
	b.WriteString(treemapHTML)
	b.WriteString(`</section>`)

	b.WriteString(`<section><h2>cycles</h2><div class="cycles">`)
	b.WriteString(importCyclesHTML)
	b.WriteString(callCyclesHTML)
	b.WriteString(`</div></section>`)

	b.WriteString(`<section><h2>hotspots ranking</h2>`)
	b.WriteString(hotspotsTableHTML)
	b.WriteString(`</section>`)

	b.WriteString(`<section><h2>architecture diagrams</h2><p class="subtitle">filled by an external script running the manifest's commands (snipe does not shell out to d2 itself)</p>`)
	b.WriteString(diagramSlotsHTML)
	b.WriteString(`</section>`)

	b.WriteString(`<footer>snipe report v1 &middot; native visuals from indexed data, D2 slots pending external render</footer>`)
	b.WriteString(`</body></html>`)
	return b.String()
}
