package output

import (
	"bytes"
	"strings"
	"testing"
)

// noEmbedResp is a minimal Claude-format def response used to probe the
// noembed self-assessment marker (snipe-ffj).
func noEmbedResp() Response[Result] {
	return Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results: []Result{
			{
				ID:    "go#main.go#main",
				Name:  "main",
				File:  "main.go",
				Range: Range{Start: Position{Line: 5, Col: 1}},
				Kind:  "func",
			},
		},
		Meta: Meta{Command: "def", IndexState: IndexFresh, Total: 1},
	}
}

func renderClaude(t *testing.T, embedMissing bool) string {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf, false, OutputClaude)
	w.SetEmbedMissing(embedMissing)
	if err := w.WriteResponse(noEmbedResp()); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	return buf.String()
}

// TestClaudeNoEmbedMarker: an index with no embeddings appends `! noembed` to
// the meta line; a healthy index stays silent (D4 — clean path pays zero).
func TestClaudeNoEmbedMarker(t *testing.T) {
	const marker = "! " + DegradedNoEmbed

	if got := renderClaude(t, true); !strings.Contains(got, marker) {
		t.Errorf("embedMissing=true: want %q in output, got:\n%s", marker, got)
	}
	if got := renderClaude(t, false); strings.Contains(got, marker) {
		t.Errorf("embedMissing=false: want no %q, got:\n%s", marker, got)
	}
}
