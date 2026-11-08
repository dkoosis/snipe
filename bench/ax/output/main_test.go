package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeAsk stands in for claude -p. Its contract with the real thing is
// TestParseClaude_RecordedReply: the bytes parseClaude reads were recorded from
// a real claude -p call. The exec itself is not run here: it needs dk's login
// and bills every call; the bench's own isolation probe exercises it per run.
func fakeAsk(answer func(arm, user string) string) ask {
	return func(_ context.Context, model, system, user string) (reply, error) {
		if system != systemPrompt {
			return reply{Text: "NONE", ModelID: "fake-" + model}, nil
		}
		arm := armText
		switch {
		case strings.Contains(user, `{"notes_head"`):
			arm = armJSON
		case strings.Contains(user, "items["):
			arm = armTOON
		case !strings.Contains(user, "\n# "):
			arm = armOneLine
		}
		return reply{Text: answer(arm, user), ModelID: "fake-" + model, InputTokens: len(user) / 4}, nil
	}
}

// Every capture parses, and rendering the parse as text gives the capture back
// (blank lines aside): the parse kept every fact, so every arm, rendered from
// it, carries the same facts.
func TestParseText_RoundTripsEveryCapture(t *testing.T) {
	paths, err := filepath.Glob("captures/*.txt")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no captures: %v", err)
	}
	nonBlank := func(s string) string {
		var keep []string
		for l := range strings.SplitSeq(s, "\n") {
			if l != "" {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		l, err := parseText(string(b))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if got, want := nonBlank(renderText(l)), nonBlank(string(b)); got != want {
			t.Errorf("%s: text render differs from capture\n got:\n%s\nwant:\n%s", p, got, want)
		}
	}
}

// show prints, for every case and arm, every name, id and location of the
// capture and every note, and no arm explains its own shape.
func TestRun_ShowCarriesEveryFactInEveryArm(t *testing.T) {
	cs, err := loadCases(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		l, err := loadListing(".", c)
		if err != nil {
			t.Fatal(err)
		}
		facts := append(slices.Clone(l.Head), l.Tail...)
		for i := range l.Items {
			it := &l.Items[i]
			facts = append(facts, it.Name, it.ID, it.Loc, it.Kind, it.Hints)
			facts = append(facts, it.Section...)
			facts = append(facts, it.Extra...)
		}
		for _, arm := range arms {
			var out, errb bytes.Buffer
			if code := run(t.Context(), []string{"show", "--bench", ".", c.ID, arm}, &out, &errb, nil); code != 0 {
				t.Fatalf("show %s %s: exit %d: %s", c.ID, arm, code, errb.String())
			}
			for _, f := range facts {
				if f != "" && !strings.Contains(out.String(), f) {
					t.Errorf("%s %s: missing %q", c.ID, arm, f)
				}
			}
			if !strings.HasSuffix(strings.TrimSpace(out.String()), "Question: "+c.Question) {
				t.Errorf("%s %s: prompt does not end with the question", c.ID, arm)
			}
		}
	}
}

// A run writes one row per case, arm, model and sample, and a report with
// accuracy and tokens per arm per model. A reply in backticks with a trailing
// period still counts as right.
func TestRun_RunReportsAccuracyAndTokensPerArmPerModel(t *testing.T) {
	cs, err := loadCases(".")
	if err != nil {
		t.Fatal(err)
	}
	accept := map[string]string{}
	for _, c := range cs {
		accept[c.Question] = c.Accept[0]
	}
	a := fakeAsk(func(arm, user string) string {
		if arm == armOneLine {
			return "no idea"
		}
		for q, ans := range accept {
			if strings.HasSuffix(user, q) {
				return "`" + ans + "`."
			}
		}
		return ""
	})
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"run", "--bench", ".", "--models", "sonnet,opus", "--samples", "3", "--parallel", "3", out}
	if code := run(t.Context(), args, &stdout, &stderr, a); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	b, err := os.ReadFile(filepath.Join(out, "calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(string(b), "\n"), len(cs)*len(arms)*2*3; got != want {
		t.Errorf("calls.jsonl has %d rows, want %d", got, want)
	}
	rep, err := os.ReadFile(filepath.Join(out, "report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	n := len(cs) * 3
	for _, model := range []string{"fake-sonnet", "fake-opus"} {
		for _, arm := range arms {
			want := "100%"
			if arm == armOneLine {
				want = "0%"
			}
			if !reportHas(string(rep), model, arm, want, n) {
				t.Errorf("report lacks %s %s at %s of %d with 3 samples per case:\n%s", model, arm, want, n, rep)
			}
		}
	}
}

func reportHas(rep, model, arm, accuracy string, n int) bool {
	for line := range strings.SplitSeq(rep, "\n") {
		f := strings.Fields(line)
		if len(f) == 7 && f[0] == model && f[1] == arm && f[3] == accuracy && strings.HasSuffix(f[2], "/"+strconv.Itoa(n)) && f[4] != "0" && f[5] == "3" {
			return true
		}
	}
	return false
}

// A model that can see dk's rules stops the run before any scored call.
func TestRun_RunStopsWhenTheProbeSeesRules(t *testing.T) {
	scored := 0
	a := func(_ context.Context, _, system, _ string) (reply, error) {
		if system == systemPrompt {
			scored++
		}
		return reply{Text: "AGENTS.md and the snipe CLI"}, nil
	}
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"run", "--bench", ".", "--models", "sonnet", out}, &stdout, &stderr, a); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if scored != 0 || !strings.Contains(stderr.String(), "isolation probe") {
		t.Errorf("scored %d calls; stderr %q", scored, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "calls.jsonl")); err == nil {
		t.Error("calls.jsonl written after a failed probe")
	}
}

// parseClaude reads a reply recorded from a real claude -p --output-format
// json call: the text, the model id and every billed input token.
func TestParseClaude_RecordedReply(t *testing.T) {
	b, err := os.ReadFile("testdata/claude-reply.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseClaude(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Text != "NONE" || r.ModelID != "claude-sonnet-5-5" || r.InputTokens != 633 {
		t.Errorf("got %+v", r)
	}
}

// A failed call bills no tokens it reports, so the mean is over answered calls
// only; the failure still counts against accuracy.
func TestRun_ReportMeansTokensOverAnsweredCalls(t *testing.T) {
	dir := t.TempDir()
	rows := `{"model":"sonnet","arm":"text","case":"a","sample":1,"correct":true,"input_tokens":1000}
{"model":"sonnet","arm":"text","case":"a","sample":2,"correct":true,"input_tokens":1000}
{"model":"sonnet","arm":"text","case":"a","sample":3,"input_tokens":0,"err":"claude -p: signal: killed"}
`
	if err := os.WriteFile(filepath.Join(dir, "calls.jsonl"), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"report", dir}, &stdout, &stderr, nil); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if f := strings.Fields(line); len(f) == 7 && f[0] == "sonnet" {
			if f[2] != "2/3" || f[4] != "1000" || f[6] != "1" {
				t.Errorf("got %q, want 2/3 right, 1000 mean tokens, 1 error", line)
			}
			return
		}
	}
	t.Errorf("no sonnet row in:\n%s", stdout.String())
}
