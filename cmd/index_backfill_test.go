package cmd

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

func TestMetricsNeedBackfill(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Fresh / pre-feature index — no marker → must backfill.
	if !metricsNeedBackfill(s) {
		t.Error("index with no metrics-version marker should need backfill")
	}

	// Current generation written → no backfill.
	if err := s.SetMeta(metaFileMetricsVersion, fileMetricsVersion); err != nil {
		t.Fatal(err)
	}
	if metricsNeedBackfill(s) {
		t.Error("index at the current metrics version should not need backfill")
	}

	// An older generation → backfill again (forward-compat for new metrics).
	if err := s.SetMeta(metaFileMetricsVersion, "0"); err != nil {
		t.Fatal(err)
	}
	if !metricsNeedBackfill(s) {
		t.Error("index at a stale metrics version should need backfill")
	}
}
