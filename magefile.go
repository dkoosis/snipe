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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	return runFoDashboard(
		// Build
		"Build/snipe:go build -o bin/snipe .",
		// Test
		"Test/unit:go test -json -cover ./...",
		// Lint - essential
		"Lint/vet:go vet ./...",
		"Lint/gofmt:gofmt -l .",
		"Lint/staticcheck:golangci-lint run --allow-parallel-runners --enable-only staticcheck ./...",
		"Lint/gosec:golangci-lint run --allow-parallel-runners --enable-only gosec ./...",
	)
}

func allCLI() error {
	fmt.Println("═══ Build + Lint + Test ═══")
	return runSequential(
		step{"Build", "go", []string{"build", "-o", "bin/snipe", "."}},
		step{"Test", "go", []string{"test", "-cover", "./..."}},
		step{"Vet", "go", []string{"vet", "./..."}},
		step{"Gofmt", "gofmt", []string{"-l", "."}},
		step{"Staticcheck", "golangci-lint", []string{"run", "--enable-only", "staticcheck", "./..."}},
		step{"Gosec", "golangci-lint", []string{"run", "--enable-only", "gosec", "./..."}},
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
	return runFoDashboard(
		// Build
		"Build/snipe:go build -o bin/snipe .",
		// Test - comprehensive
		"Test/unit:go test -json -cover ./...",
		"Test/race:go test -race -json -timeout=5m ./...",
		"Test/blackbox:go test -json -tags=blackbox ./test/blackbox/...",
		// Lint - full suite
		"Lint/golangci:golangci-lint run ./...",
		// Security
		"Security/govulncheck:govulncheck ./...",
	)
}

func qaCLI() error {
	fmt.Println("═══ Full QA ═══")
	start := time.Now()

	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}

	var failures []string
	checks := []step{
		{"Build", "go", []string{"build", "-o", "bin/snipe", "."}},
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

// Build compiles the snipe binary.
func Build() error {
	fmt.Println("→ Building snipe...")
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}

	version := getVersion()
	commit := getCommit()
	ldflags := fmt.Sprintf("-X github.com/dkoosis/snipe/cmd.Version=%s -X github.com/dkoosis/snipe/cmd.GitCommit=%s",
		version, commit)

	return sh.Run("go", "build", "-ldflags", ldflags, "-o", "bin/snipe", ".")
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
	return sh.RunV("go", "test", "-v", "-run", "TestCaptureBaseline", "./test/bench/")
}

// Bench runs Go benchmarks.
func Bench() error {
	fmt.Println("→ Running benchmarks...")
	return sh.RunV("go", "test", "-bench=.", "-benchmem", "./test/bench/")
}

// Trend shows performance metrics over time.
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
		var m struct {
			Timestamp string `json:"timestamp"`
			GitCommit string `json:"git_commit"`
			Codebase  struct {
				Symbols int `json:"symbols"`
				Refs    int `json:"refs"`
			} `json:"codebase"`
			Index struct {
				TotalMs int64 `json:"total_ms"`
			} `json:"index"`
			Query struct {
				DefByNameMs float64 `json:"def_by_name_ms"`
			} `json:"query"`
			Quality struct {
				CallGraphCoverage float64 `json:"callgraph_coverage_pct"`
			} `json:"quality"`
		}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}

		date := m.Timestamp[:10]
		commit := m.GitCommit
		if len(commit) > 7 {
			commit = commit[:7]
		}

		fmt.Printf("%-12s %-8s %8d %8d %8d %8.2f %7.1f%%\n",
			date, commit, m.Codebase.Symbols, m.Codebase.Refs,
			m.Index.TotalMs, m.Query.DefByNameMs, m.Quality.CallGraphCoverage)
	}

	return nil
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

func runFoDashboard(tasks ...string) error {
	// Find fo binary
	foBin := os.Getenv("HOME") + "/Projects/fo/bin/fo"
	if _, err := os.Stat(foBin); err != nil {
		var lookupErr error
		foBin, lookupErr = exec.LookPath("fo")
		if lookupErr != nil {
			// Fall back to CLI mode if fo not available
			fmt.Println("fo not found, falling back to CLI mode")
			return allCLI()
		}
	}

	// Ensure bin directory exists
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return fmt.Errorf("create bin dir: %w", err)
	}

	// Build task args
	args := []string{"--dashboard"}
	for _, t := range tasks {
		args = append(args, "--task", t)
	}

	// Run dashboard with TTY attached
	cmd := exec.Command(foBin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "PATH="+os.Getenv("PATH")+":"+os.Getenv("HOME")+"/go/bin")
	return cmd.Run()
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
