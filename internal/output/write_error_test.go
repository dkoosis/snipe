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
// arg, rung=notfound, and candidate_count == len(err.Candidates) — the bead's
// own AC verify line ("a NOT_FOUND row shows ... candidate_count").
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
	if r.Rung != telemetry.RungNotFound {
		t.Errorf("Rung = %q, want %q", r.Rung, telemetry.RungNotFound)
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
