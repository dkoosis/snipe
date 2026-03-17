package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/metrics"
)

// TestCaptureOrcaBaseline captures metrics for the orca codebase.
// Run with: go test -v -run TestCaptureOrcaBaseline ./test/bench/
func TestCaptureOrcaBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping orca baseline in short mode")
	}

	orcaDir := filepath.Join("..", "..", "..", "orca")
	if _, err := os.Stat(orcaDir); err != nil {
		t.Skip("orca not found at ../orca")
	}

	dir, err := filepath.Abs(orcaDir)
	if err != nil {
		t.Fatalf("resolve orca dir: %v", err)
	}

	baseline, err := metrics.Capture(metrics.CaptureConfig{
		Dir:    dir,
		Name:   "orca",
		DBPath: filepath.Join(t.TempDir(), "orca.db"),
	})
	if err != nil {
		t.Fatalf("capture baseline: %v", err)
	}

	out, err := baseline.ToJSON()
	if err != nil {
		t.Fatalf("serialize baseline: %v", err)
	}
	t.Logf("Orca Baseline Metrics:\n%s", out)

	snipeDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve snipe dir: %v", err)
	}

	writeBaselineFiles(t, snipeDir, "BASELINE_ORCA.json", "metrics_orca.jsonl", baseline, out)
}
