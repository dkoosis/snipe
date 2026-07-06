package risk

import "testing"

func TestFuse_DedupsBySignal_KeepingHighestWeight(t *testing.T) {
	t.Parallel()

	// Two `central` signals (a file in top-5, another in top-20) collapse to one
	// reason at the higher weight — a signal class fires at most once.
	v := Fuse([]Signal{
		{Signal: sigCentral, Detail: "pkg A #3", Weight: 2},
		{Signal: sigCentral, Detail: "pkg B #14", Weight: 1},
	}, ChangeStats{}, false, "")

	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 deduped reason, got %d: %+v", len(v.Reasons), v.Reasons)
	}
	if v.Reasons[0].Weight != 2 || v.Reasons[0].Detail != "pkg A #3" {
		t.Fatalf("kept wrong instance: %+v", v.Reasons[0])
	}
	if v.Score != 2 || v.Verdict != VerdictMedium {
		t.Fatalf("score=%d verdict=%q, want 2/medium", v.Score, v.Verdict)
	}
}

func TestFuse_Thresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		signals []Signal
		want    string
		score   int
	}{
		{"empty is low", nil, VerdictLow, 0},
		{"score 1 is low", []Signal{{Signal: sigChurn, Weight: 1}}, VerdictLow, 1},
		{"score 2 is medium", []Signal{{Signal: "role:api_boundary", Weight: 2}}, VerdictMedium, 2},
		{"score 4 is medium", []Signal{
			{Signal: sigCentral, Weight: 2}, {Signal: sigRolePrefix + "persistence", Weight: 2},
		}, VerdictMedium, 4},
		{"score 5 is high", []Signal{
			{Signal: sigCentral, Weight: 2}, {Signal: sigRolePrefix + "persistence", Weight: 2},
			{Signal: sigChurn, Weight: 1},
		}, VerdictHigh, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := Fuse(tt.signals, ChangeStats{}, false, "")
			if v.Score != tt.score || v.Verdict != tt.want {
				t.Fatalf("score=%d verdict=%q, want %d/%s", v.Score, v.Verdict, tt.score, tt.want)
			}
		})
	}
}

func TestFuse_DegradedForcesLow_EvenWithSignals(t *testing.T) {
	t.Parallel()

	// A degraded assessment can't be trusted to be high — force low, keep the note.
	v := Fuse([]Signal{
		{Signal: sigCentral, Weight: 2}, {Signal: sigRolePrefix + "persistence", Weight: 2},
		{Signal: sigChurn, Weight: 1},
	}, ChangeStats{}, true, "index cold")

	if v.Verdict != VerdictLow {
		t.Fatalf("degraded verdict=%q, want low", v.Verdict)
	}
	if !v.Degraded || v.Note != "index cold" {
		t.Fatalf("degraded/note not carried: %+v", v)
	}
}

func TestFuse_ReasonsAreDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	// Reasons sort by weight desc, then signal asc — stable output for golden/JSON.
	v := Fuse([]Signal{
		{Signal: sigChurn, Weight: 1},
		{Signal: "risk:concurrency", Weight: 2},
		{Signal: sigCentral, Weight: 2},
	}, ChangeStats{}, false, "")

	got := []string{v.Reasons[0].Signal, v.Reasons[1].Signal, v.Reasons[2].Signal}
	want := []string{sigCentral, "risk:concurrency", sigChurn}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v want %v", got, want)
		}
	}
}
