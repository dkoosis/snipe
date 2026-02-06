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
			Name:  repoName,
			Tasks: taskResults,
		})
	}

	// Build report
	fileAcc, symbolAcc, efficiency, meanMRR, knownGaps, byCategory := aggregateReport(results)

	report := EvalReport{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		GitCommit:  gitCommit(),
		Repos:      results,
		FileAcc:    fileAcc,
		SymbolAcc:  symbolAcc,
		Efficiency: efficiency,
		MeanMRR:    meanMRR,
		KnownGaps:  knownGaps,
		ByCategory: byCategory,
	}

	// Print console report
	fmt.Print(formatReport(report))

	// Write outputs
	writeReportJSON(t, report)
	appendReportJSONL(t, report)
	writeTaskStatus(t, report)
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
