//go:build mage

// Build and quality checks for snipe.
//
// Tiers:
//
//	mage     Build, lint, test (default)
//	mage qa  Full quality: race detection, all linters, blackbox tests
//
// Set CLI=1 for console output instead of fo dashboard.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/magefile/mage/sh"
)

var Default = All

// cli returns true if CLI=1 is set.
func cli() bool {
	return os.Getenv("CLI") != ""
}

// ----------------------------------------------------------------------------
// Tier 1: Standard (default)
// ----------------------------------------------------------------------------

// All runs build, lint, and test.
func All() error {
	if cli() {
		return allCLI()
	}
	return allDashboard()
}

func allDashboard() error {
	return runFoDashboard(allCLI, []foTask{
		{tool: "build", format: "text", cmd: "go", args: []string{"install", "."}},
		{tool: "test", format: "testjson", cmd: "go", args: []string{"test", "-json", "-cover", "./..."}},
		{tool: "vet", format: "text", cmd: "go", args: []string{"vet", "./..."}},
		{tool: "gofmt", format: "text", cmd: "gofmt", args: []string{"-l", "cmd", "internal", "test", "main.go", "magefile.go"}, failOnOutput: true},
		{tool: "staticcheck", format: "text", cmd: "golangci-lint", args: []string{"run", "--allow-parallel-runners", "--enable-only", "staticcheck", "./..."}},
	})
}

func allCLI() error {
	fmt.Println("═══ Build + Lint + Test ═══")
	return runSequential(
		step{"Build", "go", []string{"install", "."}},
		step{"Test", "go", []string{"test", "-cover", "./..."}},
		step{"Vet", "go", []string{"vet", "./..."}},
		step{"Gofmt", "gofmt", []string{"-l", "cmd", "internal", "test", "main.go", "magefile.go"}},
		step{"Staticcheck", "golangci-lint", []string{"run", "--enable-only", "staticcheck", "./..."}},
	)
}

// ----------------------------------------------------------------------------
// Tier 2: Full QA
// ----------------------------------------------------------------------------

// Qa runs comprehensive quality checks.
func Qa() error {
	if cli() {
		return qaCLI()
	}
	return qaDashboard()
}

func qaDashboard() error {
	return runFoDashboard(qaCLI, []foTask{
		{tool: "build", format: "text", cmd: "go", args: []string{"install", "."}},
		{tool: "test-unit", format: "testjson", cmd: "go", args: []string{"test", "-json", "./..."}},
		{tool: "test-race", format: "testjson", cmd: "go", args: []string{"test", "-race", "-json", "-timeout=5m", "./..."}},
		{tool: "test-blackbox", format: "testjson", cmd: "go", args: []string{"test", "-json", "-tags=blackbox", "./test/blackbox/..."}},
		{tool: "golangci", format: "text", cmd: "golangci-lint", args: []string{"run", "./..."}},
		{tool: "govulncheck", format: "text", cmd: "govulncheck", args: []string{"./..."}},
		{tool: "baseline", format: "text", cmd: "cat", args: []string{"BASELINE.json"}},
	})
}

func qaCLI() error {
	fmt.Println("═══ Full QA ═══")
	start := time.Now()

	var failures []string
	checks := []step{
		{"Build", "go", []string{"install", "."}},
		{"Test", "go", []string{"test", "-cover", "./..."}},
		{"Race", "go", []string{"test", "-race", "-timeout=5m", "./..."}},
		{"Blackbox", "go", []string{"test", "-tags=blackbox", "-v", "./test/blackbox/..."}},
		{"Golangci-lint", "golangci-lint", []string{"run", "./..."}},
		{"Govulncheck", "govulncheck", []string{"./..."}},
	}

	for _, c := range checks {
		fmt.Printf("\n▶ %s...\n", c.name)
		if err := sh.Run(c.cmd, c.args...); err != nil {
			failures = append(failures, c.name)
			fmt.Printf("  ✗ %s failed\n", c.name)
		} else {
			fmt.Printf("  ✓ %s passed\n", c.name)
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("\n═══ QA Complete - %s ═══\n", elapsed.Round(time.Second))
	if len(failures) > 0 {
		fmt.Printf("✗ FAILED: %s\n", strings.Join(failures, ", "))
		return fmt.Errorf("qa failed: %s", strings.Join(failures, ", "))
	}
	fmt.Println("✓ All checks passed")
	return nil
}

// ----------------------------------------------------------------------------
// Individual Targets
// ----------------------------------------------------------------------------

// Build installs the snipe binary to $GOPATH/bin.
func Build() error {
	fmt.Println("→ Installing snipe...")
	version := getVersion()
	commit := getCommit()
	ldflags := fmt.Sprintf("-X github.com/dkoosis/snipe/cmd.Version=%s -X github.com/dkoosis/snipe/cmd.GitCommit=%s",
		version, commit)

	return sh.Run("go", "install", "-ldflags", ldflags, ".")
}

// Test runs unit tests.
func Test() error {
	return sh.RunV("go", "test", "-cover", "./...")
}

// Lint runs golangci-lint.
func Lint() error {
	return sh.RunV("golangci-lint", "run", "./...")
}

// Blackbox runs blackbox tests against ../orca.
func Blackbox() error {
	return sh.RunV("go", "test", "-tags=blackbox", "-v", "./test/blackbox/...")
}

// Cross cross-compiles snipe for Codex/Claude cloud sandboxes (linux/amd64 + linux/arm64).
func Cross() error {
	version := getVersion()
	commit := getCommit()
	ldflags := fmt.Sprintf("-s -w -X github.com/dkoosis/snipe/cmd.Version=%s -X github.com/dkoosis/snipe/cmd.GitCommit=%s",
		version, commit)

	targets := []struct{ goos, goarch, dir string }{
		{"linux", "amd64", ".bin/linux-amd64"},
		{"linux", "arm64", ".bin/linux-arm64"},
	}

	for _, t := range targets {
		outPath := filepath.Join(t.dir, "snipe")
		if err := os.MkdirAll(t.dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", t.dir, err)
		}
		env := map[string]string{
			"CGO_ENABLED": "0",
			"GOOS":        t.goos,
			"GOARCH":      t.goarch,
		}
		if err := sh.RunWith(env, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", outPath, "."); err != nil {
			return fmt.Errorf("build %s/%s: %w", t.goos, t.goarch, err)
		}
		// Get file size for reporting
		if info, err := os.Stat(outPath); err == nil {
			fmt.Printf("  built %s (%dMB)\n", outPath, info.Size()/(1024*1024))
		}
	}
	return nil
}

// Clean removes build artifacts.
func Clean() error {
	fmt.Println("→ Cleaning...")
	_ = sh.Rm("./bin")
	_ = sh.Rm("./.snipe")
	return nil
}

// Baseline captures performance/quality metrics.
func Baseline() error {
	fmt.Println("→ Capturing baseline metrics...")
	os.Setenv("SNIPE_BASELINE", "1")
	return sh.RunV("go", "test", "-v", "-run", "TestCaptureBaseline", "./test/bench/")
}

// Bench runs Go benchmarks.
func Bench() error {
	fmt.Println("→ Running benchmarks...")
	return sh.RunV("go", "test", "-bench=.", "-benchmem", "./test/bench/")
}

// Trend shows performance metrics over time (last 30 entries).
func Trend() error {
	fmt.Println("→ Performance Trend (last 30 entries)")
	fmt.Println()

	data, err := os.ReadFile(".snipe/metrics.jsonl")
	if err != nil {
		return fmt.Errorf("no metrics history found - run 'mage baseline' first")
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Show last 30 entries
	start := 0
	if len(lines) > 30 {
		start = len(lines) - 30
	}

	fmt.Printf("%-12s %-8s %8s %8s %8s %8s %8s\n",
		"Date", "Commit", "Symbols", "Refs", "IdxMs", "DefMs", "CallCov")
	fmt.Println(strings.Repeat("-", 72))

	for _, line := range lines[start:] {
		var m metricsRecord
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}

		fmt.Printf("%-12s %-8s %8d %8d %8d %8.2f %7.1f%%\n",
			m.datePrefix(), m.shortCommit(), m.Codebase.Symbols, m.Codebase.Refs,
			m.Index.TotalMs, m.Query.DefByNameMs, m.Quality.CallGraphCoverage)
	}

	return nil
}

// EvalSetup clones benchmark repos and indexes them for eval.
func EvalSetup() error {
	fmt.Println("→ Setting up eval repos...")

	// Install snipe to $GOPATH/bin
	if err := sh.Run("go", "install", "."); err != nil {
		return fmt.Errorf("install snipe: %w", err)
	}
	snipeBin, err := exec.LookPath("snipe")
	if err != nil {
		return fmt.Errorf("snipe not found on PATH after install: %w", err)
	}

	evalDir := ".eval-repos"
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		return fmt.Errorf("create eval dir %s: %w", evalDir, err)
	}

	repos := []struct {
		Name string
		URL  string
	}{
		{"chi", "https://github.com/go-chi/chi"},
		{"cobra", "https://github.com/spf13/cobra"},
		{"bbolt", "https://github.com/etcd-io/bbolt"},
		{"fzf", "https://github.com/junegunn/fzf"},
	}

	for _, r := range repos {
		dir := filepath.Join(evalDir, r.Name)
		if _, err := os.Stat(dir); err == nil {
			fmt.Printf("  %s: already cloned\n", r.Name)
		} else {
			fmt.Printf("  %s: cloning...\n", r.Name)
			if err := sh.Run("git", "clone", "--depth=1", r.URL, dir); err != nil {
				fmt.Printf("  %s: clone failed: %v\n", r.Name, err)
				continue
			}
		}

		// Index with snipe
		fmt.Printf("  %s: indexing...\n", r.Name)
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return fmt.Errorf("resolve path %s: %w", dir, err)
		}
		cmd := exec.Command(snipeBin, "index", absDir, "--enrich=false", "--embed-mode=off")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("  %s: index failed: %v\n", r.Name, err)
			continue
		}
		fmt.Printf("  %s: ready\n", r.Name)
	}

	return nil
}

// Eval runs the localization benchmark.
func Eval() error {
	return sh.RunV("go", "test", "-v", "-tags=eval", "-run", "TestEval", "-timeout=10m", "./test/eval/")
}

// Metrics shows 30-day performance and quality metrics with sparklines.
func Metrics() error {
	data, err := os.ReadFile(".snipe/metrics.jsonl")
	if err != nil {
		return fmt.Errorf("no metrics history - run 'mage baseline' first")
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return fmt.Errorf("no metrics data")
	}

	var entries []metricsRecord
	for _, line := range lines {
		var m metricsRecord
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		entries = append(entries, m)
	}

	// Last 30 entries
	start := 0
	if len(entries) > 30 {
		start = len(entries) - 30
	}
	entries = entries[start:]

	if len(entries) == 0 {
		return fmt.Errorf("no metrics data")
	}

	// Sparkline helper: ▁▂▃▄▅▆▇█
	sparkline := func(values []float64) string {
		if len(values) == 0 {
			return ""
		}
		bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
		minV, maxV := values[0], values[0]
		for _, v := range values {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		rangeV := maxV - minV
		if rangeV == 0 {
			rangeV = 1
		}
		var result strings.Builder
		for _, v := range values {
			idx := int((v - minV) / rangeV * 7)
			if idx > 7 {
				idx = 7
			}
			result.WriteRune(bars[idx])
		}
		return result.String()
	}

	// Collect series
	var indexMs, defMs, refsMs []float64
	var symbols, refs, calls []float64
	var docCov, callCov []float64

	for _, e := range entries {
		indexMs = append(indexMs, float64(e.Index.TotalMs))
		defMs = append(defMs, e.Query.DefByNameMs)
		refsMs = append(refsMs, e.Query.RefsByIDMs)
		symbols = append(symbols, float64(e.Codebase.Symbols))
		refs = append(refs, float64(e.Codebase.Refs))
		calls = append(calls, float64(e.Codebase.CallEdges))
		docCov = append(docCov, e.Quality.DocCoverage)
		callCov = append(callCov, e.Quality.CallGraphCoverage)
	}

	curr := entries[len(entries)-1]
	first := entries[0]

	// Delta helpers
	pctChange := func(curr, prev float64) string {
		if prev == 0 {
			return ""
		}
		pct := (curr - prev) / prev * 100
		if pct > 0 {
			return fmt.Sprintf("+%.1f%%", pct)
		}
		return fmt.Sprintf("%.1f%%", pct)
	}

	intChange := func(curr, prev int) string {
		diff := curr - prev
		if diff > 0 {
			return fmt.Sprintf("+%d", diff)
		} else if diff < 0 {
			return fmt.Sprintf("%d", diff)
		}
		return "="
	}

	fmt.Println()
	fmt.Println("SNIPE METRICS (30 measurements)")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Println()

	// Performance metrics (lower is better)
	fmt.Println("PERFORMANCE                   Trend (30)                    Now      Δ")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Index Time      %s  %8dms  %s\n",
		sparkline(indexMs), curr.Index.TotalMs, pctChange(float64(curr.Index.TotalMs), float64(first.Index.TotalMs)))
	fmt.Printf("Def Query       %s  %8.3fms  %s\n",
		sparkline(defMs), curr.Query.DefByNameMs, pctChange(curr.Query.DefByNameMs, first.Query.DefByNameMs))
	fmt.Printf("Refs Query      %s  %8.3fms  %s\n",
		sparkline(refsMs), curr.Query.RefsByIDMs, pctChange(curr.Query.RefsByIDMs, first.Query.RefsByIDMs))
	fmt.Println()

	// Codebase metrics (growth)
	fmt.Println("CODEBASE                      Trend (30)                    Now      Δ")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Symbols         %s  %8d  %s\n",
		sparkline(symbols), curr.Codebase.Symbols, intChange(curr.Codebase.Symbols, first.Codebase.Symbols))
	fmt.Printf("References      %s  %8d  %s\n",
		sparkline(refs), curr.Codebase.Refs, intChange(curr.Codebase.Refs, first.Codebase.Refs))
	fmt.Printf("Call Edges      %s  %8d  %s\n",
		sparkline(calls), curr.Codebase.CallEdges, intChange(curr.Codebase.CallEdges, first.Codebase.CallEdges))
	fmt.Println()

	// Quality metrics (higher is better)
	fmt.Println("QUALITY                       Trend (30)                    Now      Δ")
	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Doc Coverage    %s  %7.1f%%  %s\n",
		sparkline(docCov), curr.Quality.DocCoverage, pctChange(curr.Quality.DocCoverage, first.Quality.DocCoverage))
	fmt.Printf("Call Coverage   %s  %7.1f%%  %s\n",
		sparkline(callCov), curr.Quality.CallGraphCoverage, pctChange(curr.Quality.CallGraphCoverage, first.Quality.CallGraphCoverage))
	fmt.Println()

	fmt.Printf("Period: %s → %s (%d measurements)\n",
		first.datePrefix(), curr.datePrefix(), len(entries))
	fmt.Println()

	return nil
}

// ----------------------------------------------------------------------------
// Shared types
// ----------------------------------------------------------------------------

// metricsRecord is the on-disk schema for .snipe/metrics.jsonl entries.
// Used by both Trend and Metrics targets.
type metricsRecord struct {
	Timestamp string `json:"timestamp"`
	GitCommit string `json:"git_commit"`
	Codebase  struct {
		Symbols   int `json:"symbols"`
		Refs      int `json:"refs"`
		CallEdges int `json:"call_edges"`
	} `json:"codebase"`
	Index struct {
		TotalMs int64 `json:"total_ms"`
	} `json:"index"`
	Query struct {
		DefByNameMs float64 `json:"def_by_name_ms"`
		RefsByIDMs  float64 `json:"refs_by_id_ms"`
	} `json:"query"`
	Quality struct {
		DocCoverage       float64 `json:"doc_coverage_pct"`
		CallGraphCoverage float64 `json:"callgraph_coverage_pct"`
	} `json:"quality"`
}

// shortCommit returns the first 7 characters of a commit hash, or the full
// string if shorter.
func (m metricsRecord) shortCommit() string {
	if len(m.GitCommit) > 7 {
		return m.GitCommit[:7]
	}
	return m.GitCommit
}

// datePrefix returns the YYYY-MM-DD prefix of Timestamp, or "unknown" if
// the timestamp is too short.
func (m metricsRecord) datePrefix() string {
	if len(m.Timestamp) >= 10 {
		return m.Timestamp[:10]
	}
	return "unknown"
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

type step struct {
	name string
	cmd  string
	args []string
}

func runSequential(steps ...step) error {
	for _, s := range steps {
		fmt.Printf("→ %s\n", s.name)
		cmd := exec.Command(s.cmd, s.args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", s.name, err)
		}
	}
	return nil
}

// foTask describes a single command whose output becomes a report section.
type foTask struct {
	tool         string // section name in the report (e.g. "test", "vet")
	format       string // "testjson", "sarif", or "text"
	cmd          string
	args         []string
	failOnOutput bool // mark text section as fail when stdout is non-empty (e.g. gofmt -l)
}

// runFoDashboard runs each task, assembles a multi-section report, and pipes it
// to fo for rendering. Falls back to the CLI function if fo is not installed.
//
// Report format: sections delimited by "--- tool:<name> format:<fmt> ---"
// See https://github.com/dkoosis/fo for details.
func runFoDashboard(fallback func() error, tasks []foTask) error {
	foBin, err := exec.LookPath("fo")
	if err != nil {
		fmt.Println("fo not found, falling back to CLI mode")
		return fallback()
	}

	var report bytes.Buffer
	for _, t := range tasks {
		cmd := exec.Command(t.cmd, t.args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()

		var status string
		content := stdout.Bytes()

		switch t.format {
		case "text":
			// Combine stderr into content for text sections
			if stderr.Len() > 0 {
				content = append(stderr.Bytes(), content...)
			}
			failed := runErr != nil || (t.failOnOutput && stdout.Len() > 0)
			if failed {
				status = " status:fail"
			} else {
				status = " status:pass"
			}
		default:
			// Structured formats (testjson, sarif): fo derives status from content
		}

		fmt.Fprintf(&report, "--- tool:%s format:%s%s ---\n", t.tool, t.format, status)
		report.Write(content)
		if len(content) == 0 || content[len(content)-1] != '\n' {
			report.WriteByte('\n')
		}
	}

	foCmd := exec.Command(foBin)
	foCmd.Stdin = &report
	foCmd.Stdout = os.Stdout
	foCmd.Stderr = os.Stderr
	return foCmd.Run()
}

func getVersion() string {
	out, err := sh.Output("git", "describe", "--tags", "--always", "--dirty")
	if err != nil || strings.TrimSpace(out) == "" {
		return "0.1.0-dev"
	}
	return strings.TrimSpace(out)
}

func getCommit() string {
	out, err := sh.Output("git", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out)
}
