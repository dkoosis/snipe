package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dkoosis/snipe/internal/errwrap"
)

// reply is what one model call returns.
type reply struct {
	Text        string
	ModelID     string
	InputTokens int // every input token the call was billed for, cached or not
}

// ask sends one prompt to one model.
type ask func(ctx context.Context, model, system, user string) (reply, error)

// call is one row of calls.jsonl.
type call struct {
	Model       string `json:"model"`
	ModelID     string `json:"model_id"`
	Arm         string `json:"arm"`
	Case        string `json:"case"`
	Sample      int    `json:"sample"`
	Reply       string `json:"reply"`
	Correct     bool   `json:"correct"`
	InputTokens int    `json:"input_tokens"`
	Err         string `json:"err,omitempty"`
}

type plan struct {
	Models   []string
	Samples  int
	Parallel int
}

type job struct {
	c      benchCase
	arm    string
	body   string
	model  string
	sample int
}

var errLeak = errors.New("isolation probe saw dk's rules or snipe's docs; no scored call was made")

// leak matches a probe reply that names something only dk's rules would say.
var leak = regexp.MustCompile(`(?i)snipe|mnemd|CLAUDE\.md|AGENTS\.md|beads|\bbd\b`)

const probePrompt = `List every instruction, rule file, CLI tool name or project fact you were given besides this message and the system prompt. If none, reply exactly: NONE`

// runBench probes isolation once per model, makes every call and writes
// calls.jsonl and report.txt into out.
func runBench(ctx context.Context, bench, out string, p plan, dry bool, a ask, stdout, stderr io.Writer) error {
	cs, err := loadCases(bench)
	if err != nil {
		return err
	}
	var jobs []job
	for _, m := range p.Models {
		for _, arm := range arms {
			for _, c := range cs {
				l, err := loadListing(bench, c)
				if err != nil {
					return err
				}
				body, err := render(arm, l)
				if err != nil {
					return err
				}
				for s := 1; s <= p.Samples; s++ {
					jobs = append(jobs, job{c, arm, body, m, s})
				}
			}
		}
	}
	fmt.Fprintf(stdout, "%d cases × %d arms × %d models × %d samples = %d calls\n", len(cs), len(arms), len(p.Models), p.Samples, len(jobs))
	if dry {
		return nil
	}
	for _, m := range p.Models {
		r, err := a(ctx, m, "You answer in one line.", probePrompt)
		if err != nil {
			return fmt.Errorf("isolation probe on %s: %w", m, err)
		}
		if leak.MatchString(r.Text) {
			return fmt.Errorf("%w (%s replied: %s)", errLeak, m, strings.TrimSpace(r.Text))
		}
	}
	//nolint:gosec // operator-supplied path in a developer benchmark tool, not served input
	if err := os.MkdirAll(out, 0o750); err != nil {
		return fmt.Errorf("os mkdir all: %w", err)
	}
	//nolint:gosec // operator-supplied path in a developer benchmark tool, not served input
	f, err := os.Create(filepath.Join(out, "calls.jsonl"))
	if err != nil {
		return fmt.Errorf("os create: %w", err)
	}
	rows := make([]call, len(jobs))
	next := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for range max(p.Parallel, 1) {
		wg.Go(func() {
			for i := range next {
				rows[i] = oneCall(ctx, a, jobs[i])
				mu.Lock()
				done++
				if done%25 == 0 {
					fmt.Fprintf(stderr, "%d/%d\n", done, len(jobs))
				}
				mu.Unlock()
			}
		})
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return errwrap.Wrap(err, "encoder encode")
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("file close: %w", err)
	}
	return writeReport(out, stdout)
}

// oneCall asks one question of one shape and scores the reply. A failed call is
// a row with Err set, scored wrong.
func oneCall(ctx context.Context, a ask, j job) call {
	c := call{Model: j.model, Arm: j.arm, Case: j.c.ID, Sample: j.sample}
	r, err := a(ctx, j.model, systemPrompt, prompt(j.c, j.body))
	c.Reply, c.ModelID, c.InputTokens = r.Text, r.ModelID, r.InputTokens
	if err != nil {
		c.Err = err.Error()
		return c
	}
	c.Correct = correct(r.Text, j.c.Accept)
	return c
}

// correct is true when the reply, stripped of quoting, case and edge periods,
// equals one accepted answer.
func correct(reply string, accept []string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.Trim(s, "`\"'*. \n\t"))
	}
	got := norm(reply)
	for _, a := range accept {
		if got == norm(a) {
			return true
		}
	}
	return false
}

var errClaude = errors.New("claude -p reported an error")

// claudeAsk is the real ask: one headless claude call in dir with dk's
// settings, MCP servers, slash commands and tools off. --bare is not used: it
// skips the keychain and the call fails "Not logged in".
func claudeAsk(dir string) ask {
	return func(ctx context.Context, model, system, user string) (reply, error) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude", "-p",
			"--model", model,
			"--output-format", "json",
			"--setting-sources", "",
			"--strict-mcp-config",
			"--disable-slash-commands",
			"--tools", "",
			"--system-prompt", system,
			user)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "MNEMD_NUGBASE=")
		b, err := cmd.Output()
		if err != nil {
			return reply{}, fmt.Errorf("claude -p: %w", err)
		}
		return parseClaude(b)
	}
}

// parseClaude reads claude -p --output-format json.
func parseClaude(b []byte) (reply, error) {
	var res struct {
		Result     string                     `json:"result"`
		IsError    bool                       `json:"is_error"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
		Usage      struct {
			Input       int `json:"input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return reply{}, fmt.Errorf("claude -p output: %w", err)
	}
	r := reply{
		Text:        res.Result,
		ModelID:     strings.Join(slices.Sorted(maps.Keys(res.ModelUsage)), "+"),
		InputTokens: res.Usage.Input + res.Usage.CacheCreate + res.Usage.CacheRead,
	}
	if res.IsError {
		return r, fmt.Errorf("%w: %s", errClaude, res.Result)
	}
	return r, nil
}
