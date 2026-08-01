package cmd

import "time"

// reportManifestVersion is bumped whenever the manifest JSON shape changes in
// a way an external filler script would need to notice.
const reportManifestVersion = 1

// reportSlotIDArch, reportSlotIDFlow, reportSlotIDLifecycle are the
// placeholder-div ids shared between the manifest (report_manifest.go) and
// the HTML shell (report_html.go) — the external script matches a manifest
// slot's "id" to the HTML element it injects SVG into, so the two must never
// drift; both sides build off reportDiagramSlots below.
const (
	reportSlotIDArch      = "diagram-arch"
	reportSlotIDFlow      = "diagram-flow"
	reportSlotIDLifecycle = "diagram-lifecycle"
)

// reportManifestSlot is one D2 diagram artifact `snipe report` cannot render
// itself (sn-qnq3 design: snipe owns the shell + manifest, an external script
// owns D2->SVG rendering, keeping the `d2` binary dependency out of snipe).
type reportManifestSlot struct {
	ID           string   `json:"id"`
	ArtifactType string   `json:"artifact_type"` // arch|flow|lifecycle
	Description  string   `json:"description"`
	Command      string   `json:"command"`
	ArgsRequired []string `json:"args_required,omitempty"`
}

// reportManifest is the machine-readable companion to report.html: for each
// D2 placeholder slot in the shell, which `snipe diagram` command to run
// (piped into `d2 -`, per the ratified sn-qnq3 design) to produce the SVG the
// external filler script then injects into that slot's element.
type reportManifest struct {
	Version     int                  `json:"version"`
	GeneratedAt string               `json:"generated_at"`
	Slots       []reportManifestSlot `json:"slots"`
}

// reportDiagramSlots is the single source of truth for v1's three D2 slots —
// both buildReportManifest and the HTML shell builder read from it, so a slot
// id can never exist on one side and not the other.
func reportDiagramSlots() []reportManifestSlot {
	return []reportManifestSlot{
		{
			ID:           reportSlotIDArch,
			ArtifactType: "arch",
			Description:  "Package import graph, top packages by PageRank",
			Command:      "snipe diagram arch | d2 -",
		},
		{
			ID:           reportSlotIDFlow,
			ArtifactType: "flow",
			Description:  "Depth-limited call graph from an entry symbol",
			Command:      "snipe diagram flow <entry-symbol> | d2 -",
			ArgsRequired: []string{"entry-symbol"},
		},
		{
			ID:           reportSlotIDLifecycle,
			ArtifactType: "lifecycle",
			Description:  "CRUD trace (create/mutate/read/delete) for a type",
			Command:      "snipe diagram lifecycle <type-name> | d2 -",
			ArgsRequired: []string{"type-name"},
		},
	}
}

// buildReportManifest assembles the manifest for a report generated at
// `generatedAt`. Pure function (no I/O) so manifest shape is unit-testable
// without an index or filesystem.
func buildReportManifest(generatedAt time.Time) reportManifest {
	return reportManifest{
		Version:     reportManifestVersion,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Slots:       reportDiagramSlots(),
	}
}
