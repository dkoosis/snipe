package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/telemetry"
)

// A directory with no .git or go.mod falls back to CWD as its root (snipe
// index <path> supports it); OpenStore must point telemetry there too, or
// risk's usage row is dropped while its risk.jsonl record is kept.
func TestRisk_RootlessDirCountsItsUsageRow(t *testing.T) {
	t.Setenv("SNIPE_NO_TELEMETRY", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".snipe"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	telemetry.SetRoot(filepath.Join(t.TempDir(), "elsewhere")) // a stale sink from before
	t.Cleanup(func() { telemetry.SetRoot("") })

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	stdout := os.Stdout
	os.Stdout = devnull
	err = runRisk("main", "HEAD")
	os.Stdout = stdout
	if err != nil {
		t.Fatalf("runRisk: %v", err)
	}

	rows, err := telemetry.ReadAll(dir)
	if err != nil {
		t.Fatalf("read %s/.snipe/usage.jsonl: %v", dir, err)
	}
	if len(rows) != 1 || rows[0].Command != cmdNameRisk {
		t.Fatalf("usage rows = %+v, want one %s row", rows, cmdNameRisk)
	}
}
