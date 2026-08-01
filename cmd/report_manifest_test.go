package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildReportManifestShape(t *testing.T) {
	ts := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	m := buildReportManifest(ts)

	if m.Version != reportManifestVersion {
		t.Errorf("Version = %d, want %d", m.Version, reportManifestVersion)
	}
	if m.GeneratedAt != "2026-08-01T12:00:00Z" {
		t.Errorf("GeneratedAt = %q, want RFC3339 UTC timestamp", m.GeneratedAt)
	}
	if len(m.Slots) != 3 {
		t.Fatalf("got %d slots, want 3 (arch, flow, lifecycle)", len(m.Slots))
	}

	wantTypes := map[string]bool{"arch": false, "flow": false, "lifecycle": false}
	for _, s := range m.Slots {
		if s.ID == "" {
			t.Errorf("slot %+v has empty ID", s)
		}
		if s.Command == "" {
			t.Errorf("slot %s has empty Command", s.ID)
		}
		if _, ok := wantTypes[s.ArtifactType]; !ok {
			t.Errorf("slot %s has unexpected ArtifactType %q", s.ID, s.ArtifactType)
		}
		wantTypes[s.ArtifactType] = true
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("no slot found for artifact type %q", typ)
		}
	}
}

// TestReportManifestCommandsMentionD2Pipe locks in the sn-qnq3 design: snipe
// does not shell out to `d2` itself, so every manifest command must be a
// `snipe diagram ...` invocation the EXTERNAL script pipes into `d2 -`.
func TestReportManifestCommandsMentionD2Pipe(t *testing.T) {
	m := buildReportManifest(time.Now())
	for _, s := range m.Slots {
		if !strings.Contains(s.Command, "snipe diagram") || !strings.Contains(s.Command, "| d2 -") {
			t.Errorf("slot %s command %q should run `snipe diagram ...` piped into `d2 -`", s.ID, s.Command)
		}
	}
}

// TestReportManifestArgsRequiredMatchesTemplate ensures any slot declaring
// ArgsRequired actually has a placeholder token in its command — a slot that
// claims an arg is needed but whose command has nothing to fill in would
// silently mislead the external filler script.
func TestReportManifestArgsRequiredMatchesTemplate(t *testing.T) {
	m := buildReportManifest(time.Now())
	for _, s := range m.Slots {
		for _, arg := range s.ArgsRequired {
			token := "<" + arg + ">"
			if !strings.Contains(s.Command, token) {
				t.Errorf("slot %s declares arg %q but command %q has no %q placeholder", s.ID, arg, s.Command, token)
			}
		}
	}
}

// TestReportManifestSlotIDsMatchHTMLSlots pins the manifest<->HTML shell
// contract: every slot id the manifest lists must appear as an `id="..."` in
// the rendered diagram-slots HTML, or the external script has nothing to
// inject the SVG into.
func TestReportManifestSlotIDsMatchHTMLSlots(t *testing.T) {
	m := buildReportManifest(time.Now())
	html := renderDiagramSlotsHTML(m.Slots)
	for _, s := range m.Slots {
		want := `id="` + s.ID + `"`
		if !strings.Contains(html, want) {
			t.Errorf("manifest slot %s has no matching %s in the rendered HTML shell", s.ID, want)
		}
	}
}

// TestBuildReportManifestJSONRoundTrip guards the "machine-readable manifest"
// acceptance criterion directly: the struct must marshal to valid JSON an
// external script can parse (field names as documented, not zero-valued away).
func TestBuildReportManifestJSONRoundTrip(t *testing.T) {
	m := buildReportManifest(time.Now())
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	for _, field := range []string{"version", "generated_at", "slots"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("manifest JSON missing field %q: %s", field, data)
		}
	}
	slots, ok := decoded["slots"].([]interface{})
	if !ok || len(slots) != 3 {
		t.Fatalf("manifest JSON slots = %v, want array of 3", decoded["slots"])
	}
}
