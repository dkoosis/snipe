package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/telemetry"
)

// TestWriteResponse_EmitsSessionKey guards the shared WriteResponse -> Emit
// path (internal/output/json.go) every Response[Result] consumer goes
// through, including def (sn-yhl4). Investigation found no live defect —
// every def code path calls OpenStore, which calls telemetry.SetSessionKey,
// before any WriteResponse/WriteErrorWithMeta call that could emit a row —
// but the mechanism itself had no direct test. This closes that gap.
func TestWriteResponse_EmitsSessionKey(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	telemetry.SetRoot(root)
	telemetry.SetSessionKey("sess-abc")
	t.Cleanup(func() {
		telemetry.SetRoot("")
		telemetry.SetSessionKey("")
	})

	resp := Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results: []Result{
			{ID: "abc123", File: "main.go", Kind: "func", Name: "main"},
		},
		Meta: Meta{
			Command:    "def",
			IndexState: IndexFresh,
			Total:      1,
		},
	}

	w := NewWriter(&strings.Builder{}, OutputClaude)
	if err := w.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse: %v", err)
	}

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
	}
	if recs[0].SessionKey != "sess-abc" {
		t.Errorf("SessionKey = %q, want %q", recs[0].SessionKey, "sess-abc")
	}
}
