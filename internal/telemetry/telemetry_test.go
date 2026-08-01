package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmitAndReadAll_RoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	SetRoot(root)
	SetCaller("test")
	SetSessionKey("sess-abc")
	t.Cleanup(func() { SetRoot(""); SetCaller(""); SetSessionKey("") })

	Emit(Fields{Command: "def", Outcome: "ok", Ms: 12, Arg: "Foo", Rung: RungExact, IndexState: "fresh"})
	Emit(Fields{Command: "def", Outcome: "NOT_FOUND", Ms: 3, Arg: "Fooo", Rung: RungNotFound, CandidateCount: 2, TriedRungs: []string{"lookup:name"}})
	Emit(Fields{Command: "pack", Outcome: "ok", Ms: 40, Arg: "Bar", Rung: RungFuzzy})

	recs, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	if recs[0].Command != "def" || recs[0].Outcome != "ok" || recs[0].Caller != "test" {
		t.Errorf("first record = %+v", recs[0])
	}
	if recs[0].Arg != "Foo" {
		t.Errorf("first record Arg = %q, want %q", recs[0].Arg, "Foo")
	}
	if recs[0].Rung != RungExact {
		t.Errorf("first record Rung = %q, want %q", recs[0].Rung, RungExact)
	}
	if recs[0].SessionKey != "sess-abc" {
		t.Errorf("first record SessionKey = %q, want %q", recs[0].SessionKey, "sess-abc")
	}
	if recs[0].IndexState != "fresh" {
		t.Errorf("first record IndexState = %q, want %q", recs[0].IndexState, "fresh")
	}

	if recs[1].Outcome != "NOT_FOUND" {
		t.Errorf("second outcome = %q, want NOT_FOUND", recs[1].Outcome)
	}
	if recs[1].Rung != RungNotFound {
		t.Errorf("second record Rung = %q, want %q", recs[1].Rung, RungNotFound)
	}
	if recs[1].CandidateCount != 2 {
		t.Errorf("second record CandidateCount = %d, want 2", recs[1].CandidateCount)
	}
	if len(recs[1].TriedRungs) != 1 || recs[1].TriedRungs[0] != "lookup:name" {
		t.Errorf("second record TriedRungs = %v, want [lookup:name]", recs[1].TriedRungs)
	}
}

func TestReadAll_SkipsMalformedLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".snipe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"ts":"2026-06-12T10:00:00Z","cmd":"def","outcome":"ok","ms":5}
not json at all
{"ts":"2026-06-12T10:00:01Z","cmd":"refs","outcome":"ok","ms":7}
`
	if err := os.WriteFile(filepath.Join(dir, "usage.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2 (malformed line skipped)", len(recs))
	}
}

// TestReadAll_BackwardCompat_PreEnrichmentLines guards the AC's "no schema
// break" requirement (sn-r1do.1): a usage.jsonl written by a pre-enrichment
// binary (only ts/cmd/outcome/ms/caller — sn-g0d.7's shape) must still parse
// cleanly, with every new field simply zero-valued.
func TestReadAll_BackwardCompat_PreEnrichmentLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".snipe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"ts":"2026-06-12T10:00:00Z","cmd":"def","outcome":"ok","ms":5,"caller":"orca"}
`
	if err := os.WriteFile(filepath.Join(dir, "usage.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := ReadAll(root)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Command != "def" || r.Outcome != "ok" || r.Ms != 5 || r.Caller != "orca" {
		t.Errorf("pre-enrichment fields not preserved: %+v", r)
	}
	if r.Arg != "" || r.Rung != "" || r.CandidateCount != 0 || r.IndexState != "" || r.SessionKey != "" || r.TriedRungs != nil {
		t.Errorf("enrichment fields should zero-value on an old-shaped line, got %+v", r)
	}
}

func TestEmit_DisabledByEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	SetRoot(root)
	t.Cleanup(func() { SetRoot("") })
	t.Setenv("SNIPE_NO_TELEMETRY", "1")

	Emit(Fields{Command: "def", Outcome: "ok", Ms: 1})

	if _, err := os.Stat(filepath.Join(root, ".snipe", "usage.jsonl")); !os.IsNotExist(err) {
		t.Error("usage.jsonl should not exist when SNIPE_NO_TELEMETRY is set")
	}
}

// TestClassifyRung_AllSixValues exercises ClassifyRung against decision-path
// shapes actually emitted by cmd/def.go and cmd/search.go (not guessed —
// verified by reading those files, sn-r1do.1 plan §2).
func TestClassifyRung_AllSixValues(t *testing.T) {
	tests := []struct {
		name         string
		decisionPath []string
		degraded     []string
		outcome      string
		want         string
	}{
		{"silent exact (def lookup:name)", []string{"lookup:name"}, nil, "ok", RungExact},
		{"silent exact (empty path)", nil, nil, "ok", RungExact},
		{"lookup:id", []string{"lookup:id"}, nil, "ok", RungExact},
		{"lookup:position", []string{"lookup:position"}, nil, "ok", RungExact},
		{"ci-match degraded marker wins", nil, []string{"ci-match"}, "ok", RungCaseInsensitive},
		{"lookup:name_select", []string{"lookup:name_select"}, nil, "ok", RungMethod},
		{"lookup:file_qualified", []string{"lookup:file_qualified"}, nil, "ok", RungMethod},
		{"search index_fallback", []string{"rg:0_results", "index_fallback:3_results"}, nil, "ok", RungFuzzy},
		{"search name_augment", []string{"name_augment:2_added"}, nil, "ok", RungFuzzy},
		{"search sim fallback", []string{"rg:0_results", "sim:2_results:40ms"}, nil, "ok", RungSemantic},
		{"search sim_augment", []string{"sim_augment:1_added:12ms"}, nil, "ok", RungSemantic},
		{"not found", nil, nil, "NOT_FOUND", RungNotFound},
		{"ambiguous symbol", nil, nil, "AMBIGUOUS_SYMBOL", RungNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyRung(tt.decisionPath, tt.degraded, tt.outcome)
			if got != tt.want {
				t.Errorf("ClassifyRung(%v, %v, %q) = %q, want %q", tt.decisionPath, tt.degraded, tt.outcome, got, tt.want)
			}
		})
	}
}

// TestResolveSessionKey_EnvVarWins confirms the CLAUDE_CODE_SESSION_ID env
// var is used verbatim when present (sn-r1do.1 §4) — the fallback bucket
// path must not run when it's set.
func TestResolveSessionKey_EnvVarWins(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-session-123")
	got := ResolveSessionKey("/some/root")
	if got != "cc-session-123" {
		t.Errorf("ResolveSessionKey = %q, want %q", got, "cc-session-123")
	}
}

// TestResolveSessionKey_FallbackIncludesRoot guards the synthetic
// cwd+time-bucket fallback used outside Claude Code (direct terminal/CI).
func TestResolveSessionKey_FallbackIncludesRoot(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	got := ResolveSessionKey("/some/root")
	if len(got) <= len("/some/root") {
		t.Errorf("ResolveSessionKey fallback = %q, want it to embed root %q", got, "/some/root")
	}
}
