package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/telemetry"
)

// TestWriteErrorWithMeta_EmitsCandidateCountAndArg drives an AMBIGUOUS_SYMBOL
// error through WriteErrorWithMeta and confirms the usage.jsonl row carries
// arg, rung=ambiguous (sn-r1do.6), candidate_count == len(err.Candidates) and
// the candidates' kinds.
func TestWriteErrorWithMeta_EmitsCandidateCountAndArg(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	telemetry.SetRoot(root)
	t.Cleanup(func() { telemetry.SetRoot("") })

	candidates := []protocol.Candidate{
		{ID: "a", Name: "Config", File: "a/config.go", Kind: "type"},
		{ID: "b", Name: "Config", File: "b/config.go", Kind: "type"},
	}
	w := NewWriter(&strings.Builder{}, OutputClaude)
	_ = w.WriteErrorWithMeta("def", "Config", []string{"lookup:name"}, protocol.IndexFresh, len(candidates), protocol.NewAmbiguousError("Config", candidates))

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
	}
	r := recs[0]
	if r.Arg != "Config" {
		t.Errorf("Arg = %q, want %q", r.Arg, "Config")
	}
	if r.Rung != telemetry.RungAmbiguous {
		t.Errorf("Rung = %q, want %q", r.Rung, telemetry.RungAmbiguous)
	}
	if len(r.CandidateKinds) != 1 || r.CandidateKinds["type"] != 2 {
		t.Errorf("CandidateKinds = %v, want map[type:2]", r.CandidateKinds)
	}
	if r.CandidateCount != 2 {
		t.Errorf("CandidateCount = %d, want 2", r.CandidateCount)
	}
	if r.Outcome != protocol.ErrAmbiguousSymbol {
		t.Errorf("Outcome = %q, want %q", r.Outcome, protocol.ErrAmbiguousSymbol)
	}
}

// TestWriteErrorWithMeta_NotFoundDidYouMeanCandidateCount guards the gap this
// bead exists to close: NewNotFoundError's "did you mean" suggestions are
// plain strings, not []Candidate (that field is AMBIGUOUS_SYMBOL-only), so
// candidate_count for a fuzzy-suggested NOT_FOUND must come from an explicit
// caller-supplied count, not len(err.Candidates) (which is always 0 here).
func TestWriteErrorWithMeta_NotFoundDidYouMeanCandidateCount(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	telemetry.SetRoot(root)
	t.Cleanup(func() { telemetry.SetRoot("") })

	suggestions := []string{"ClassifyRung"}
	w := NewWriter(&strings.Builder{}, OutputClaude)
	_ = w.WriteErrorWithMeta("def", "ClasifyRung", nil, protocol.IndexFresh, len(suggestions), protocol.NewNotFoundError("ClasifyRung", suggestions...))

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
	}
	if recs[0].CandidateCount != 1 {
		t.Errorf("CandidateCount = %d, want 1 (from the did-you-mean suggestion, not err.Candidates which is empty here)", recs[0].CandidateCount)
	}
}

// TestWriteError_MissRowsCarryQueryArg guards the sn-r1do.3 finding: refs,
// callers, callees, tests, impact, impl, explain, types and lifecycle report
// NOT_FOUND / AMBIGUOUS_SYMBOL through the thin WriteError wrapper, which had
// no query arg — so their usage.jsonl miss rows were arg-blind and no miss
// class could be counted across sessions. The constructors now remember the
// query, so every WriteError caller emits it without a per-site edit.
func TestWriteError_MissRowsCarryQueryArg(t *testing.T) {
	cases := []struct {
		name      string
		err       *protocol.Error
		wantArg   string
		wantCount int
	}{
		{"not found", protocol.NewNotFoundError("reapUnder"), "reapUnder", 0},
		{"not found with suggestions", protocol.NewNotFoundError("ClasifyRung", "ClassifyRung", "ClassifyRungs"), "ClasifyRung", 2},
		{"ambiguous", protocol.NewAmbiguousError("settle", []protocol.Candidate{
			{ID: "a", Name: "settle", File: "a.go", Kind: "func"},
			{ID: "b", Name: "settle", File: "b.go", Kind: "method", Receiver: "*Base"},
		}), "settle", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
				t.Fatal(err)
			}
			telemetry.SetRoot(root)
			t.Cleanup(func() { telemetry.SetRoot("") })

			w := NewWriter(&strings.Builder{}, OutputClaude)
			_ = w.WriteError("tests", tc.err)

			recs, err := telemetry.ReadAll(root)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
			}
			if recs[0].Arg != tc.wantArg {
				t.Errorf("Arg = %q, want %q", recs[0].Arg, tc.wantArg)
			}
			if recs[0].CandidateCount != tc.wantCount {
				t.Errorf("CandidateCount = %d, want %d", recs[0].CandidateCount, tc.wantCount)
			}
		})
	}
}

// TestWriteError_AmbiguousRowCountsCandidateKinds guards sn-r1do.6: an
// ambiguous row logs rung "ambiguous" and its candidates' kinds, so a func-vs-
// methods tie a deterministic rule could break shows up in the mining.
func TestWriteError_AmbiguousRowCountsCandidateKinds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	telemetry.SetRoot(root)
	t.Cleanup(func() { telemetry.SetRoot("") })

	w := NewWriter(&strings.Builder{}, OutputClaude)
	_ = w.WriteError("refs", protocol.NewAmbiguousError("Close", []protocol.Candidate{
		{ID: "a", Name: "Close", File: "a.go", Kind: "func"},
		{ID: "b", Name: "Close", File: "b.go", Kind: "method", Receiver: "*Store"},
		{ID: "c", Name: "Close", File: "c.go", Kind: "method", Receiver: "*Writer"},
		{ID: "d", Name: "Close", File: "d.go", Kind: "method", Receiver: "*Conn"},
	}))

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
	}
	r := recs[0]
	if r.Rung != telemetry.RungAmbiguous {
		t.Errorf("Rung = %q, want %q", r.Rung, telemetry.RungAmbiguous)
	}
	if len(r.CandidateKinds) != 2 || r.CandidateKinds["func"] != 1 || r.CandidateKinds["method"] != 3 {
		t.Errorf("CandidateKinds = %v, want map[func:1 method:3]", r.CandidateKinds)
	}
	if r.Error != "" {
		t.Errorf("Error = %q, want empty on a non-internal error", r.Error)
	}
}

// TestWriteError_InternalErrorRowLogsMessage guards sn-r1do.6: an
// INTERNAL_ERROR row carries its message, truncated, and no rung (nothing was
// resolved); other error codes do not log the message.
func TestWriteError_InternalErrorRowLogsMessage(t *testing.T) {
	cases := []struct {
		name      string
		err       *protocol.Error
		wantError string
	}{
		{"internal short", &protocol.Error{Code: protocol.ErrInternal, Message: "open db: disk I/O error"}, "open db: disk I/O error"},
		{"internal long", &protocol.Error{Code: protocol.ErrInternal, Message: strings.Repeat("x", 500)}, strings.Repeat("x", telemetry.MaxErrorLen-3) + "..."},
		{"invalid args", &protocol.Error{Code: protocol.ErrInvalidArgs, Message: "need a symbol"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
				t.Fatal(err)
			}
			telemetry.SetRoot(root)
			t.Cleanup(func() { telemetry.SetRoot("") })

			w := NewWriter(&strings.Builder{}, OutputClaude)
			_ = w.WriteError("boundary", tc.err)

			recs, err := telemetry.ReadAll(root)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("got %d usage.jsonl rows, want 1", len(recs))
			}
			if recs[0].Error != tc.wantError {
				t.Errorf("Error = %q, want %q", recs[0].Error, tc.wantError)
			}
			if recs[0].Rung != "" {
				t.Errorf("Rung = %q, want empty (nothing was resolved)", recs[0].Rung)
			}
		})
	}
}

// TestWriteResponse_NoDecisionPathLogsNoRung guards sn-r1do.6: an ok response
// with no decision path (boundary, lits, refs, ...) logs no rung instead of
// defaulting to exact, and still logs its arg.
func TestWriteResponse_NoDecisionPathLogsNoRung(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	telemetry.SetRoot(root)
	t.Cleanup(func() { telemetry.SetRoot("") })

	w := NewWriter(&strings.Builder{}, OutputJSON)
	_ = w.WriteResponse(protocol.Response[protocol.Result]{
		Ok:   true,
		Meta: protocol.Meta{Command: "boundary", Query: map[string]string{"a": "cmd", "b": "internal/store", "direction": "both"}},
	})
	_ = w.WriteResponse(protocol.Response[protocol.Result]{
		Ok:   true,
		Meta: protocol.Meta{Command: "def", Query: map[string]string{"id": "0123456789abcdef"}, DecisionPath: []string{"lookup:id"}},
	})

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d usage.jsonl rows, want 2", len(recs))
	}
	if recs[0].Rung != "" || recs[0].Arg != "cmd internal/store" {
		t.Errorf("boundary row rung=%q arg=%q, want rung empty, arg %q", recs[0].Rung, recs[0].Arg, "cmd internal/store")
	}
	if recs[1].Rung != telemetry.RungExact || recs[1].Arg != "0123456789abcdef" {
		t.Errorf("def-by-id row rung=%q arg=%q, want exact, 0123456789abcdef", recs[1].Rung, recs[1].Arg)
	}
}
