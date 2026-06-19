package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestClaudeMatchTierMarker: a degraded match tier carried on Meta.Degraded
// renders as a `! <tier>` meta line; an exact (served) result emits nothing
// (D4 — the common path pays zero). Mirrors the noembed marker (snipe-ffj).
func TestClaudeMatchTierMarker(t *testing.T) {
	render := func(degraded ...string) string {
		var buf bytes.Buffer
		w := NewWriter(&buf, false, OutputClaude)
		resp := Response[Result]{
			Protocol: ProtocolVersion,
			Ok:       true,
			Results: []Result{{
				ID: "go#main.go#main", Name: "main", File: "main.go",
				Range: Range{Start: Position{Line: 5, Col: 1}}, Kind: "func",
			}},
			Meta: Meta{Command: "def", IndexState: IndexFresh, Total: 1, Degraded: degraded},
		}
		if err := w.WriteResponse(resp); err != nil {
			t.Fatalf("WriteResponse: %v", err)
		}
		return buf.String()
	}

	if got := render(DegradedCIMatch); !strings.Contains(got, "! "+DegradedCIMatch) {
		t.Errorf("ci-match: want %q in output, got:\n%s", "! "+DegradedCIMatch, got)
	}
	if got := render(); strings.Contains(got, "! ci-match") {
		t.Errorf("served: want no tier marker, got:\n%s", got)
	}
}

// TestClaudeSemanticMarker: a semantic-fallback search emits `! semantic:0.62`
// carrying the top hit's cosine (snipe-ffj). It also pins the Phase 0 finding —
// Result.Score is otherwise dropped from default Claude output, so the marker
// is the only place that magnitude survives.
func TestClaudeSemanticMarker(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputClaude)
	resp := Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results: []Result{{
			ID: "go#t.go#Near", Name: "Near", File: "t.go",
			Range: Range{Start: Position{Line: 1, Col: 1}}, Kind: "func",
			Score: 0.62,
		}},
		Meta: Meta{
			Command: "search", IndexState: IndexNotUsed, Total: 1,
			Degraded: []string{SemanticMarker(0.62)},
		},
	}
	if err := w.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "! semantic:0.62") {
		t.Errorf("want %q in output, got:\n%s", "! semantic:0.62", got)
	}
	// Phase 0: the raw Score never renders on its own — only via the marker.
	if strings.Count(got, "0.62") != 1 {
		t.Errorf("want score magnitude to appear once (in the marker only), got:\n%s", got)
	}
}
