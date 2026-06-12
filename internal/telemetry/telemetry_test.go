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
	t.Cleanup(func() { SetRoot(""); SetCaller("") })

	Emit("def", "ok", 12)
	Emit("def", "NOT_FOUND", 3)
	Emit("pack", "ok", 40)

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
	if recs[1].Outcome != "NOT_FOUND" {
		t.Errorf("second outcome = %q, want NOT_FOUND", recs[1].Outcome)
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

func TestEmit_DisabledByEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	SetRoot(root)
	t.Cleanup(func() { SetRoot("") })
	t.Setenv("SNIPE_NO_TELEMETRY", "1")

	Emit("def", "ok", 1)

	if _, err := os.Stat(filepath.Join(root, ".snipe", "usage.jsonl")); !os.IsNotExist(err) {
		t.Error("usage.jsonl should not exist when SNIPE_NO_TELEMETRY is set")
	}
}
