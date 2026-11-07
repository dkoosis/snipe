package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/protocol"
)

// noEmbedResp is a minimal Claude-format response from command, used to probe
// the noembed self-assessment marker (snipe-ffj).
func noEmbedResp(command string) protocol.Response[protocol.Result] {
	return protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:    "go#main.go#main",
				Name:  "main",
				File:  "main.go",
				Range: protocol.Range{Start: protocol.Position{Line: 5, Col: 1}},
				Kind:  "func",
			},
		},
		Meta: protocol.Meta{Command: command, IndexState: protocol.IndexFresh, Total: 1},
	}
}

func renderClaude(t *testing.T, command string, embedMissing bool) string {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf, OutputClaude)
	w.SetEmbedMissing(embedMissing)
	if err := w.WriteResponse(noEmbedResp(command)); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}
	return buf.String()
}

// TestClaudeNoEmbedMarker: an index with no embeddings appends `! noembed` to
// the meta line of a verb that can use embeddings; a healthy index stays
// silent (D4 — clean path pays zero), and so does every verb that never
// reads embeddings (sn-lvub.13).
func TestClaudeNoEmbedMarker(t *testing.T) {
	const marker = "! " + protocol.DegradedNoEmbed

	for _, cmd := range []string{"search", "sim"} {
		if got := renderClaude(t, cmd, true); !strings.Contains(got, marker) {
			t.Errorf("%s, embedMissing=true: want %q in output, got:\n%s", cmd, marker, got)
		}
		if got := renderClaude(t, cmd, false); strings.Contains(got, marker) {
			t.Errorf("%s, embedMissing=false: want no %q, got:\n%s", cmd, marker, got)
		}
	}
	for _, cmd := range []string{"callers", "refs", "def"} {
		if got := renderClaude(t, cmd, true); strings.Contains(got, marker) {
			t.Errorf("%s never reads embeddings: want no %q, got:\n%s", cmd, marker, got)
		}
	}
}
