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
	if got := render(DegradedMethodMatch); !strings.Contains(got, "! "+DegradedMethodMatch) {
		t.Errorf("method-match: want %q in output, got:\n%s", "! "+DegradedMethodMatch, got)
	}
	if got := render(); strings.Contains(got, "! ci-match") || strings.Contains(got, "! method-match") {
		t.Errorf("served: want no tier marker, got:\n%s", got)
	}
}
