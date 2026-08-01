package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/dkoosis/snipe/internal/telemetry"
	"github.com/dkoosis/snipe/internal/util"
)

const kindUsage = "usage"

// runUsageMetrics summarizes .snipe/usage.jsonl: invocations and error rate
// per command, plus p50 latency. This is the ground truth for "is snipe the
// tool Claude reaches for" — compare across time, not against vibes.
func runUsageMetrics() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := util.FindProjectRoot(cwd)
	if root == "" {
		root = cwd
	}

	recs, err := telemetry.ReadAll(root)
	if err != nil {
		fmt.Println("no usage data yet (.snipe/usage.jsonl) — run some queries first")
		return nil //nolint:nilerr // Missing telemetry is a fresh state, not a failure
	}

	type agg struct {
		calls  int
		errors int
		ms     []int64
	}
	byCmd := make(map[string]*agg)
	total, totalErr := 0, 0
	for i := range recs {
		r := &recs[i]
		a := byCmd[r.Command]
		if a == nil {
			a = &agg{}
			byCmd[r.Command] = a
		}
		a.calls++
		total++
		if r.Outcome != "ok" {
			a.errors++
			totalErr++
		}
		a.ms = append(a.ms, r.Ms)
	}

	cmds := make([]string, 0, len(byCmd))
	for c := range byCmd {
		cmds = append(cmds, c)
	}
	sort.Slice(cmds, func(i, j int) bool { return byCmd[cmds[i]].calls > byCmd[cmds[j]].calls })

	fmt.Printf("usage: %d invocations, %d errors (%.0f%%)\n", total, totalErr, pct(totalErr, total))
	for _, c := range cmds {
		a := byCmd[c]
		fmt.Printf("  %-12s %5d calls  %3d err  p50 %dms\n", c, a.calls, a.errors, p50(a.ms))
	}
	return nil
}

func pct(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func p50(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	return ms[len(ms)/2]
}
