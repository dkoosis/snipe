package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// The AX bench (bench/ax, run by mnemd's axbench) asks models to route a
// situation to a snipe command from the help text alone (sn-lvub.2). These
// tests keep its inputs honest: current.txt is what --help prints, and every
// situation and accept names only verbs the help offers.

const axBench = "../bench/ax"

// renderRootHelp is `snipe --help` as Execute builds it.
func renderRootHelp(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	parser, err := kong.New(&CLI{},
		kong.Name("snipe"),
		kong.Description(rootHelp),
		rootHelpOptions,
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatalf("build kong parser: %v", err)
	}
	if _, err := parser.Parse([]string{"--help"}); err != nil && buf.Len() == 0 {
		t.Fatalf("parse --help: %v", err)
	}
	return buf.String()
}

// TestHelpVariantCurrent fails when --help drifts from the bench's copy, so the
// bench never measures a CLI that does not exist. Fix: go run . --help >
// bench/ax/variants/current.txt, then update the "current" accepts in
// bench/ax/requests.jsonl.
func TestHelpVariantCurrent(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(axBench, "variants", "current.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := renderRootHelp(t); got != string(want) {
		t.Errorf("bench/ax/variants/current.txt is not what --help prints\n--- file\n%s\n--- help\n%s", want, got)
	}
}

// helpVerb is a command line in kong's grouped help: two spaces, the verb.
var helpVerb = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9-]*)(?:\s|$)`)

func variantVerbs(t *testing.T) map[string][]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(axBench, "variants", "*.txt"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no variants in %s: %v", axBench, err)
	}
	out := map[string][]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p) // #nosec G304 -- test fixture path
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(p), ".txt")
		for _, m := range helpVerb.FindAllStringSubmatch(string(b), -1) {
			if !slices.Contains(out[name], m[1]) {
				out[name] = append(out[name], m[1])
			}
		}
	}
	return out
}

type axAccept struct {
	Verb string `json:"verb"`
}

type axRequest struct {
	ID         string                `json:"id"`
	Situation  string                `json:"situation"`
	Accept     map[string][]axAccept `json:"accept"`
	Defensible map[string][]axAccept `json:"defensible"`
}

// TestAXBench_RequestsUseOnlyTheHelp enforces the bench's rules for snipe:
// mnemd's axbench finds verbs only on "  mnemd <verb>" lines, so on kong's
// help its own banned-verb check sees nothing. A situation must name no verb
// from any variant; each request accepts something under every variant; an
// accept or defensible verb is one that variant's help offers.
func TestAXBench_RequestsUseOnlyTheHelp(t *testing.T) {
	verbs := variantVerbs(t)
	var all []string
	for _, vs := range verbs {
		for _, v := range vs {
			if !slices.Contains(all, v) {
				all = append(all, v)
			}
		}
	}
	f, err := os.Open(filepath.Join(axBench, "requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var r axRequest
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("requests.jsonl line %d: %v", n+1, err)
		}
		n++
		for _, v := range all {
			if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(v) + `\b`).MatchString(r.Situation) {
				t.Errorf("%s: situation names the verb %q", r.ID, v)
			}
		}
		for variant, vs := range verbs {
			if len(r.Accept[variant]) == 0 {
				t.Errorf("%s: no accept for variant %q", r.ID, variant)
			}
			for _, a := range append(slices.Clone(r.Accept[variant]), r.Defensible[variant]...) {
				if !slices.Contains(vs, a.Verb) {
					t.Errorf("%s: %q is not a verb in variant %q", r.ID, a.Verb, variant)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if n < 40 {
		t.Errorf("requests.jsonl holds %d situations, want at least 40", n)
	}
}
