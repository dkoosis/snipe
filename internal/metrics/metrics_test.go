package metrics_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/metrics"
)

func TestComparison_SetsStatus_When_MetricsCompared(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		current         *metrics.Baseline
		reference       *metrics.Baseline
		cfg             metrics.CompareConfig
		wantStatus      string
		wantMessage     string
		wantHasFailure  bool
		wantThreshold   float64
		wantCheckName   string
		wantMessagePart string
	}{
		{
			name: "error: lower is better regresses",
			current: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 130},
			},
			reference: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 100},
			},
			cfg:             metrics.CompareConfig{Threshold: 15},
			wantStatus:      "fail",
			wantHasFailure:  true,
			wantCheckName:   "index_time",
			wantMessagePart: "increased",
		},
		{
			name: "boundary: zero reference passes",
			current: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 10},
			},
			reference: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 0},
			},
			cfg:            metrics.CompareConfig{Threshold: 10},
			wantStatus:     "pass",
			wantMessage:    "no reference value",
			wantHasFailure: false,
			wantCheckName:  "index_time",
		},
		{
			name: "boundary: zero config threshold uses default",
			current: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 110},
			},
			reference: &metrics.Baseline{
				Index: metrics.IndexMetrics{TotalMs: 100},
			},
			cfg:            metrics.CompareConfig{},
			wantStatus:     "warn",
			wantHasFailure: false,
			wantCheckName:  "index_time",
			wantThreshold:  15.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			comp := metrics.Compare(tt.current, tt.reference, tt.cfg)

			check, ok := findCheck(comp.Checks, tt.wantCheckName)
			require.True(t, ok, "expected %s check to be present", tt.wantCheckName)

			assert.Equal(t, tt.wantStatus, check.Status)
			if tt.wantMessage != "" {
				assert.Equal(t, tt.wantMessage, check.Message)
			}
			if tt.wantMessagePart != "" {
				assert.Contains(t, check.Message, tt.wantMessagePart)
			}
			if tt.wantThreshold != 0 {
				assert.Equal(t, tt.wantThreshold, check.Threshold)
			}
			assert.Equal(t, tt.wantHasFailure, comp.HasFailure)
		})
	}
}

func TestLoadHistory_SkipsMalformedLines_When_JSONLContainsInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		historyPath string
		want        []metrics.Baseline
	}{
		{
			name:        "error: malformed line is skipped",
			historyPath: filepath.Join("testdata", "history.jsonl"),
			want: []metrics.Baseline{
				{
					Timestamp: "2024-01-01T00:00:00Z",
					GitCommit: "abc123",
					Index:     metrics.IndexMetrics{TotalMs: 100},
					Codebase:  metrics.CodebaseStats{Symbols: 10},
					Query:     metrics.QueryMetrics{DefByNameMs: 5},
					Quality:   metrics.QualityMetrics{DocCoverage: 20},
				},
				{
					Timestamp: "2024-01-02T00:00:00Z",
					GitCommit: "def456",
					Index:     metrics.IndexMetrics{TotalMs: 130},
					Codebase:  metrics.CodebaseStats{Symbols: 12},
					Query:     metrics.QueryMetrics{DefByNameMs: 7},
					Quality:   metrics.QualityMetrics{DocCoverage: 25},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			history, err := metrics.LoadHistory(tt.historyPath)
			require.NoError(t, err)

			if diff := cmp.Diff(tt.want, history); diff != "" {
				t.Fatalf("history mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadBaseline_ReturnsExpectedResult_When_ReadingBaseline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) string
		want    *metrics.Baseline
		wantErr bool
	}{
		{
			name: "error: file missing",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "missing.json")
			},
			wantErr: true,
		},
		{
			name: "success: valid baseline loads",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				path := filepath.Join(dir, "baseline.json")
				baseline := metrics.Baseline{
					Timestamp: "2024-01-01T00:00:00Z",
					GitCommit: "abc123",
					Index:     metrics.IndexMetrics{TotalMs: 42},
				}
				require.NoError(t, writeBaselineFile(path, baseline))
				return path
			},
			want: &metrics.Baseline{
				Timestamp: "2024-01-01T00:00:00Z",
				GitCommit: "abc123",
				Index:     metrics.IndexMetrics{TotalMs: 42},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t)

			got, err := metrics.LoadBaseline(path)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("baseline mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHistoryEntries_SetsDeltas_When_MetricsChangeAcrossRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		baselines []metrics.Baseline
		want      []metrics.HistoryEntry
	}{
		{
			name: "success: deltas reflect regressions and improvements",
			baselines: []metrics.Baseline{
				{
					Timestamp: "2024-01-01T00:00:00Z",
					GitCommit: "abc123",
					Index:     metrics.IndexMetrics{TotalMs: 100},
					Codebase:  metrics.CodebaseStats{Symbols: 10},
					Query:     metrics.QueryMetrics{DefByNameMs: 10},
					Quality:   metrics.QualityMetrics{DocCoverage: 20},
				},
				{
					Timestamp: "2024-01-02T00:00:00Z",
					GitCommit: "def456",
					Index:     metrics.IndexMetrics{TotalMs: 120},
					Codebase:  metrics.CodebaseStats{Symbols: 11},
					Query:     metrics.QueryMetrics{DefByNameMs: 12},
					Quality:   metrics.QualityMetrics{DocCoverage: 22},
				},
				{
					Timestamp: "2024-01-03T00:00:00Z",
					GitCommit: "ghi789",
					Index:     metrics.IndexMetrics{TotalMs: 80},
					Codebase:  metrics.CodebaseStats{Symbols: 12},
					Query:     metrics.QueryMetrics{DefByNameMs: 9},
					Quality:   metrics.QualityMetrics{DocCoverage: 23},
				},
			},
			want: []metrics.HistoryEntry{
				{
					Timestamp:   "2024-01-01T00:00:00Z",
					GitCommit:   "abc123",
					IndexMs:     100,
					Symbols:     10,
					QueryP95Ms:  10,
					DocCoverage: 20,
				},
				{
					Timestamp:   "2024-01-02T00:00:00Z",
					GitCommit:   "def456",
					IndexMs:     120,
					Symbols:     11,
					QueryP95Ms:  12,
					DocCoverage: 22,
					IndexDelta:  "↑",
					QueryDelta:  "↑",
				},
				{
					Timestamp:   "2024-01-03T00:00:00Z",
					GitCommit:   "ghi789",
					IndexMs:     80,
					Symbols:     12,
					QueryP95Ms:  9,
					DocCoverage: 23,
					IndexDelta:  "↓",
					QueryDelta:  "↓",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := metrics.ToHistoryEntries(tt.baselines)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("history entries mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func findCheck(checks []metrics.Check, name string) (metrics.Check, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return metrics.Check{}, false
}

func writeBaselineFile(path string, baseline metrics.Baseline) error {
	data, err := baseline.ToJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
