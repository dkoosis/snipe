package cmd

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// TestRunHealCmdHonorsCtxCancel drives runHealCmd with a slow subprocess and a
// context that cancels after ~50ms. The heal must return promptly (the killed
// subprocess), not block ~15s on the healTimeout backstop. Guards snipe-7ba:
// self-heal must honor cmdCtx so a query's --timeout bounds wall-clock.
func TestRunHealCmdHonorsCtxCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep(1)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// A subprocess that would run far longer than the ctx timeout (and the 15s
	// backstop). CommandContext + the <-ctx.Done() arm must kill it early.
	cmd := exec.CommandContext(ctx, "sleep", "30")

	start := time.Now()
	ok := runHealCmd(ctx, cmd)
	elapsed := time.Since(start)

	if ok {
		t.Errorf("runHealCmd returned true on ctx cancel; want false")
	}
	if elapsed > 2*time.Second {
		t.Errorf("runHealCmd blocked %v on ctx cancel; want prompt return (~50ms), not the 15s backstop", elapsed)
	}
}

// TestRunHealCmdCleanFinish confirms a fast subprocess that exits 0 yields true.
func TestRunHealCmdCleanFinish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses true(1)")
	}
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "true")
	if !runHealCmd(ctx, cmd) {
		t.Errorf("runHealCmd returned false for a clean exit; want true")
	}
}

func TestShouldHeal(t *testing.T) {
	cases := []struct {
		name    string
		changed int
		want    bool
	}{
		{"no drift", 0, false},
		{"one file", 1, true},
		{"at cap", healMaxChangedFiles, true},
		{"over cap warns instead", healMaxChangedFiles + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldHeal(tc.changed); got != tc.want {
				t.Errorf("shouldHeal(%d) = %v, want %v", tc.changed, got, tc.want)
			}
		})
	}
}
