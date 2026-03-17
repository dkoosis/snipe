package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/metrics"
)

// TestCaptureBaseline captures all metrics for snipe's own codebase and outputs JSON.
// Run with: SNIPE_BASELINE=1 go test -v -run TestCaptureBaseline ./test/bench/
func TestCaptureBaseline(t *testing.T) {
	if os.Getenv("SNIPE_BASELINE") == "" {
		t.Skip("skipping baseline capture (set SNIPE_BASELINE=1 to run)")
	}

	dir, err := filepath.Abs(testRepo)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	baseline, err := metrics.Capture(metrics.CaptureConfig{
		Dir:    dir,
		Name:   "snipe",
		DBPath: filepath.Join(t.TempDir(), "snipe.db"),
	})
	if err != nil {
		t.Fatalf("capture baseline: %v", err)
	}

	out, err := baseline.ToJSON()
	if err != nil {
		t.Fatalf("serialize baseline: %v", err)
	}
	t.Logf("Baseline Metrics:\n%s", out)

	writeBaselineFiles(t, dir, "BASELINE.json", "metrics.jsonl", baseline, out)
}

// writeBaselineFiles writes the baseline JSON and appends to JSONL history.
func writeBaselineFiles(t *testing.T, repoDir, baselineName, historyName string, baseline *metrics.Baseline, pretty []byte) {
	t.Helper()

	baselineFile := filepath.Join(repoDir, baselineName)
	if err := os.WriteFile(baselineFile, pretty, 0644); err != nil {
		t.Logf("Warning: could not write baseline file: %v", err)
	} else {
		t.Logf("Baseline written to: %s", baselineFile)
	}

	historyFile := filepath.Join(repoDir, ".snipe", historyName)
	if err := os.MkdirAll(filepath.Dir(historyFile), 0755); err != nil {
		t.Logf("Warning: could not create history dir: %v", err)
		return
	}

	line, err := baseline.ToJSONL()
	if err != nil {
		t.Logf("Warning: could not serialize JSONL: %v", err)
		return
	}

	f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Logf("Warning: could not open history file: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		t.Logf("Warning: could not write history line: %v", err)
		return
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		t.Logf("Warning: could not write history newline: %v", err)
	}
	t.Logf("History appended to: %s", historyFile)
}
