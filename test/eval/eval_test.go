//go:build eval

package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestEval(t *testing.T) {
	benchmarkPath := filepath.Join(repoRoot, "docs", "eval", "benchmark.yaml")
	data, err := os.ReadFile(benchmarkPath)
	if err != nil {
		t.Fatalf("read benchmark.yaml: %v", err)
	}

	var bench BenchmarkFile
	if err := yaml.Unmarshal(data, &bench); err != nil {
		t.Fatalf("parse benchmark.yaml: %v", err)
	}

	repoTasks := map[string][]Task{
		"orca":  bench.Orca,
		"chi":   bench.Chi,
		"cobra": bench.Cobra,
		"bbolt": bench.Bbolt,
		"fzf":   bench.Fzf,
	}

	var results []RepoResult
	repoOrder := []string{"chi", "cobra", "bbolt", "fzf", "orca"}

	for _, repoName := range repoOrder {
		tasks := repoTasks[repoName]
		dir := resolveRepoDir(repoName)

		if dir == "" {
			t.Logf("SKIP repo %s: not available (run 'mage EvalSetup' to clone)", repoName)
			results = append(results, RepoResult{
				Name:    repoName,
				Skipped: true,
			})
			continue
		}

		checkIndexFreshness(t, dir)
		t.Logf("EVAL repo %s: %s (%d tasks)", repoName, dir, len(tasks))

		var taskResults []TaskResult
		for _, task := range tasks {
			tr := runTask(t, dir, task)
			taskResults = append(taskResults, tr)

			status := "PASS"
			if tr.KnownGap {
				status = "SKIP"
			} else if !tr.SymbolAccuracy || !tr.FileAccuracy {
				status = "FAIL"
			}
			t.Logf("  %s %s (file=%v sym=%v eff=%v mrr=%.2f)",
				status, task.ID, tr.FileAccuracy, tr.SymbolAccuracy, tr.CallEfficiency, tr.MRR)
		}

		results = append(results, RepoResult{
			Name:       repoName,
			Tasks:      taskResults,
			Embeddings: countEmbeddings(t, dir),
		})
	}

	// Build report
	fileAcc, symbolAcc, efficiency, meanMRR, knownGaps, byCategory := aggregateReport(results)
	wFileAcc, wSymbolAcc, wEfficiency := computeWeightedScores(byCategory, bench.Weights)

	report := EvalReport{
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		GitCommit:          gitCommit(),
		Repos:              results,
		FileAcc:            fileAcc,
		SymbolAcc:          symbolAcc,
		Efficiency:         efficiency,
		MeanMRR:            meanMRR,
		KnownGaps:          knownGaps,
		ByCategory:         byCategory,
		WeightedFileAcc:    wFileAcc,
		WeightedSymbolAcc:  wSymbolAcc,
		WeightedEfficiency: wEfficiency,
	}

	// Print console report
	fmt.Print(formatReport(report))

	// Write outputs
	writeReportJSON(t, report)
	appendReportJSONL(t, report)
	writeTaskStatus(t, report)

	// Enforce gates — the report labels above are cosmetic; these fail the build.
	enforceGates(t, report)
}

// Gate thresholds. File/Symbol/Efficiency lock in current performance as a
// regression floor. MRR (rank quality) is the discriminating axis now that the
// binary metrics are saturated — floor set just below the measured baseline so
// a ranking regression fails the build. See sn-4bxp.
const (
	gateFileAcc   = 90.0
	gateSymbolAcc = 75.0
	gateEff       = 80.0
	// gateMRR floors rank quality just below the measured baseline (0.76 after the
	// sn-4bxp MRR-stressor tasks landed). Any ranking regression fails the build;
	// improving retrieval ranking is how this number climbs back toward 1.0.
	gateMRR = 0.72
)

// countEmbeddings reports how many vector embeddings the repo's index holds,
// or -1 if the count is unavailable. Zero means the semantic-ranking path was
// not exercised — a caveat the report surfaces so MRR isn't over-read.
func countEmbeddings(t *testing.T, repoDir string) int {
	t.Helper()
	stdout, _, code := run(t, repoDir, "embed-status")
	if code != 0 {
		return -1
	}
	var resp embedStatusResponse
	if err := json.Unmarshal(stdout, &resp); err != nil || len(resp.Results) == 0 {
		return -1
	}
	return resp.Results[0].Total
}

// enforceGates fails the test when any aggregate metric drops below its floor.
func enforceGates(t *testing.T, report EvalReport) {
	t.Helper()
	check := func(name string, value, floor float64, pct bool) {
		if value < floor {
			unit := ""
			if pct {
				unit = "%"
			}
			t.Errorf("GATE FAIL: %s = %.2f%s below floor %.2f%s", name, value, unit, floor, unit)
		}
	}
	check("File accuracy", report.FileAcc, gateFileAcc, true)
	check("Symbol accuracy", report.SymbolAcc, gateSymbolAcc, true)
	check("Efficiency", report.Efficiency, gateEff, true)
	check("Mean MRR", report.MeanMRR, gateMRR, false)
}

// runTask executes all commands for a task and scores the results.
func runTask(t *testing.T, repoDir string, task Task) TaskResult {
	t.Helper()

	var responses []snipeResponse

	for _, cmdStr := range task.Commands {
		args := parseCommand(cmdStr)
		stdout, stderr, exitCode := run(t, repoDir, args...)

		var resp snipeResponse
		if err := json.Unmarshal(stdout, &resp); err != nil {
			// Couldn't parse JSON — record as error
			responses = append(responses, snipeResponse{
				Error: &snipeError{
					Code:    "PARSE_ERROR",
					Message: fmt.Sprintf("exit=%d parse=%v stderr=%s", exitCode, err, truncate(string(stderr), 200)),
				},
			})
			continue
		}

		// Pack commands return nested results (definition/references/callers/callees).
		// The flat snipeResponse parse loses them. Re-parse as pack and flatten.
		if resp.Meta.Command == "pack" || (resp.Ok && len(resp.Results) == 0 && task.Category == "pack") {
			if flat, ok := flattenPackResponse(stdout); ok {
				resp = flat
			}
		}

		responses = append(responses, resp)
	}

	return scoreTask(task, responses)
}

// parseCommand splits a command string into snipe CLI args.
// Handles quoted strings: `search 'FTS'` → ["search", "FTS"]
func parseCommand(cmd string) []string {
	var args []string
	var current strings.Builder
	inQuote := byte(0)

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			} else {
				current.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			inQuote = ch
		case ch == ' ':
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func gitCommit() string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func writeReportJSON(t *testing.T, report EvalReport) {
	t.Helper()

	outDir := filepath.Join(repoRoot, "docs", "eval")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("warning: could not create eval output dir: %v", err)
		return
	}

	data, err := reportJSON(report)
	if err != nil {
		t.Logf("warning: marshal report: %v", err)
		return
	}

	path := filepath.Join(outDir, "EVAL_RESULTS.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Logf("warning: write %s: %v", path, err)
		return
	}
	t.Logf("wrote %s", path)
}

func appendReportJSONL(t *testing.T, report EvalReport) {
	t.Helper()

	snipeDir := filepath.Join(repoRoot, ".snipe")
	if err := os.MkdirAll(snipeDir, 0o755); err != nil {
		t.Logf("warning: could not create .snipe dir: %v", err)
		return
	}

	data, err := reportJSONL(report)
	if err != nil {
		t.Logf("warning: marshal report jsonl: %v", err)
		return
	}
	data = append(data, '\n')

	path := filepath.Join(snipeDir, "eval.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Logf("warning: open %s: %v", path, err)
		return
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		t.Logf("warning: write %s: %v", path, err)
		return
	}
	t.Logf("appended to %s", path)
}

// writeTaskStatus writes per-task status lines to docs/eval/task_status.txt.
// On subsequent runs, this enables easy diff to spot regressions.
func writeTaskStatus(t *testing.T, report EvalReport) {
	t.Helper()

	outDir := filepath.Join(repoRoot, "docs", "eval")
	path := filepath.Join(outDir, "task_status.txt")

	// Read previous status for diff reporting
	prevStatus := map[string]string{}
	if prev, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(prev), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				prevStatus[fields[0]] = fields[1]
			}
		}
	}

	var b strings.Builder
	for _, repo := range report.Repos {
		if repo.Skipped {
			continue
		}
		for _, tr := range repo.Tasks {
			status := "PASS"
			if tr.KnownGap {
				status = "SKIP"
			} else if !tr.SymbolAccuracy || !tr.FileAccuracy {
				status = "FAIL"
			}

			line := fmt.Sprintf("%-24s %s", tr.ID, status)

			// Add diff annotation
			if prev, ok := prevStatus[tr.ID]; ok && prev != status {
				line += fmt.Sprintf("  (was: %s)", prev)
			}

			if tr.KnownGap {
				line += "  (known_gap)"
			}

			b.WriteString(line + "\n")
		}
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Logf("warning: write %s: %v", path, err)
		return
	}
	t.Logf("wrote %s", path)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
