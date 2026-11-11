// Command output is the output-reading bench (sn-lvub.12): it shows a model one
// captured snipe result list in one shape and asks a question with a checkable
// answer. See bench/ax/README.md.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dkoosis/snipe/internal/errwrap"

	"github.com/dkoosis/atomicfile"
)

// main runs each claude call in a fresh empty directory, so no CLAUDE.md or
// AGENTS.md is in reach.
func main() {
	dir, err := os.MkdirTemp("", "axout-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "output:", err)
		os.Exit(1)
	}
	code := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, claudeAsk(dir))
	_ = os.Remove(dir)
	os.Exit(code)
}

const usage = `usage:
  output capture --snipe BIN --repo DIR [--bench DIR]   rerun each case's snipe command into captures/
  output show [--bench DIR] CASE ARM                    print what the model sees
  output run [--bench DIR] [--models sonnet,opus] [--samples 3] [--parallel 4] [--dry-run] OUTDIR
  output report OUTDIR                                  rewrite OUTDIR/report.txt from calls.jsonl`

// run is the binary: it returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, a ask) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	bench := fs.String("bench", "bench/ax/output", "bench directory (cases.jsonl, captures/)")
	snipe := fs.String("snipe", "snipe", "snipe binary to capture with")
	repo := fs.String("repo", ".", "repo the captures run in")
	models := fs.String("models", "sonnet,opus", "comma-separated claude model aliases")
	samples := fs.Int("samples", 3, "samples per case, arm and model")
	parallel := fs.Int("parallel", 4, "calls in flight")
	dry := fs.Bool("dry-run", false, "print the plan and make no call")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	var err error
	switch args[0] {
	case "capture":
		err = capture(ctx, *bench, *snipe, *repo, stdout)
	case "show":
		err = show(*bench, fs.Args(), stdout)
	case "run":
		if fs.NArg() != 1 {
			err = fmt.Errorf("run wants one OUTDIR: %w", errUsage)
			break
		}
		p := plan{Models: strings.Split(*models, ","), Samples: *samples, Parallel: *parallel}
		err = runBench(ctx, *bench, fs.Arg(0), p, *dry, a, stdout, stderr)
	case "report":
		if fs.NArg() != 1 {
			err = fmt.Errorf("report wants one OUTDIR: %w", errUsage)
			break
		}
		err = writeReport(fs.Arg(0), stdout)
	default:
		err = fmt.Errorf("unknown command %q\n%s: %w", args[0], usage, errUsage)
	}
	if err != nil {
		fmt.Fprintln(stderr, "output:", err)
		return 1
	}
	return 0
}

// benchCase is one line of cases.jsonl: a snipe command, a question about its
// output and the answers that count as right.
type benchCase struct {
	ID       string   `json:"id"`
	Args     []string `json:"args"`
	Question string   `json:"question"`
	Accept   []string `json:"accept"`
}

// capturePath is where a case's text capture lives; cases sharing a command
// share it.
func capturePath(bench string, args []string) string {
	return filepath.Join(bench, "captures", strings.Join(args, "_")+".txt")
}

func loadCases(bench string) ([]benchCase, error) {
	//nolint:gosec // operator-supplied path in a developer benchmark tool, not served input
	f, err := os.Open(filepath.Join(bench, "cases.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("os open: %w", err)
	}
	defer f.Close()
	var cs []benchCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var c benchCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("cases.jsonl: %w", err)
		}
		if c.ID == "" || len(c.Args) == 0 || c.Question == "" || len(c.Accept) == 0 {
			return nil, fmt.Errorf("cases.jsonl: case %q needs id, args, question and accept: %w", c.ID, errListing)
		}
		cs = append(cs, c)
	}
	return cs, errwrap.Wrap(sc.Err(), "scanner err")
}

// loadListing reads and parses a case's capture.
func loadListing(bench string, c benchCase) (listing, error) {
	b, err := os.ReadFile(capturePath(bench, c.Args))
	if err != nil {
		return listing{}, fmt.Errorf("case %s: %w (run capture)", c.ID, err)
	}
	l, err := parseText(string(b))
	if err != nil {
		return listing{}, fmt.Errorf("case %s: %w", c.ID, err)
	}
	return l, nil
}

// capture reruns each distinct case command in repo. Suggestions are off: they
// are advice about the next command, not facts the questions ask about.
func capture(ctx context.Context, bench, snipe, repo string, stdout io.Writer) error {
	cs, err := loadCases(bench)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, c := range cs {
		path := capturePath(bench, c.Args)
		if done[path] {
			continue
		}
		done[path] = true
		cmd := exec.CommandContext(ctx, snipe, append(slices.Clone(c.Args), "--format", "concise", "--no-suggestions")...) //nolint:gosec // the bench runs the snipe it is told to
		cmd.Dir = repo
		var errb bytes.Buffer
		cmd.Stderr = &errb
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("snipe %s: %w: %s", strings.Join(c.Args, " "), err, errb.String())
		}
		if _, err := parseText(string(out)); err != nil {
			return fmt.Errorf("snipe %s: %w", strings.Join(c.Args, " "), err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("os mkdir all: %w", err)
		}
		if err := atomicfile.WriteFile(path, out, 0o600); err != nil {
			return fmt.Errorf("atomicfile write file: %w", err)
		}
		fmt.Fprintln(stdout, "wrote", path)
	}
	return nil
}

func show(bench string, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("show wants CASE ARM: %w", errUsage)
	}
	cs, err := loadCases(bench)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.ID != args[0] {
			continue
		}
		l, err := loadListing(bench, c)
		if err != nil {
			return err
		}
		body, err := render(args[1], l)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, prompt(c, body))
		return nil
	}
	return fmt.Errorf("no case %q: %w", args[0], errUsage)
}

// systemPrompt is the same for every arm: no arm is told how its shape reads.
const systemPrompt = `You are an agent working in a Go repository. You ran a code-navigation command and its output follows. Answer the question from that output alone. Reply with only the answer on one line: no explanation, no code fence.`

func prompt(c benchCase, body string) string {
	return "$ snipe " + strings.Join(c.Args, " ") + "\n" + body + "\nQuestion: " + c.Question
}
