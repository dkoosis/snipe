// Package script runs testscript (.txtar) smoke tests against the snipe CLI.
//
// Coexistence with test/blackbox — the two suites test DIFFERENT surfaces and
// intentionally overlap on command names:
//
//   - blackbox (build tag `blackbox`, run() forces --format json) = the JSON
//     envelope CONTRACT: field shapes, ok/error semantics, result payloads.
//   - this suite (no build tag, plain `make`) = the DEFAULT CLI surface SMOKE:
//     exit code + one stable line of whatever the command prints by DEFAULT
//     (Claude-text for most; the default-JSON commands — edit/status/doctor —
//     assert a stable envelope key). It is a cheap regression net for "does the
//     command run and print its recognizable output on the surface Claude reads".
//
// Because they cover different surfaces, a command with a blackbox JSON test
// still earns a default-surface smoke here — the two are not redundant.
//
// Gap note (snipe-p61): auditing the full cmd/cli.go command set against both
// suites, only hotspots, sensitive, and guard were covered by NEITHER — those
// are the primary contracts added here. The rest of the added scripts smoke the
// default text surface of commands whose JSON contract lives in blackbox.
//
// Exemptions (a real happy-path assertion is impossible on this fixture; the
// script smokes a documented fallback and says why, in its own header comment):
//   - sim: zero-embedding index + no Voyage key → graceful no-key path.
//   - risk / index: risk needs a git work tree; index is already run by Setup
//     for every script → --help fast-exit, like watch.
//   - imports: a FILE-argument command → --help fast-exit (real path is
//     blackbox TestImports). The original reason, a copied index whose stored
//     file paths no longer existed, is gone: each script now indexes in place.
//   - verify / tests / show: the fixture has no diff / no _test.go / stable hex
//     ID to assert → graceful degrade path (JSON contract is in blackbox).
//
// Pattern: build-and-exec. cmd.Execute() calls os.Exit internally (kong's
// FatalIfErrorf), so the binary cannot be driven in-process. Instead the suite
// builds snipe once and, for each script, writes and indexes a small Go fixture
// in $WORK and prepends the binary's directory to PATH so scripts can
// `exec snipe ...` against a ready-to-query index. No build tag — it
// is its own package, so plain `make` (go test ./...) runs it.
package script

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/dkoosis/snipe/internal/store"
)

func TestScripts(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	// Build the snipe binary once into a shared temp dir.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "snipe")
	build := exec.Command("go", "build", "-o", binPath, "./")
	build.Dir = repoRoot
	var bout, berr bytes.Buffer
	build.Stdout = &bout
	build.Stderr = &berr
	if err := build.Run(); err != nil {
		t.Fatalf("build snipe: %v\n%s\n%s", err, bout.String(), berr.String())
	}

	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"cmpjson":    cmpJSON,
			"seedembed":  seedEmbed,
			"jsonsubset": jsonSubset,
			"linesubset": lineSubset,
		},
		Setup: func(env *testscript.Env) error {
			// Prepend the snipe binary dir so scripts can `exec snipe`.
			env.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			// Per-script HOME isolation so any caches don't collide.
			home := filepath.Join(env.WorkDir, "home")
			if err := os.MkdirAll(home, 0o755); err != nil {
				return err
			}
			env.Setenv("HOME", home)

			// Disable keychain access in the snipe binary under test: a real
			// snipe/voyage keychain item on a developer Mac would otherwise
			// supply credentials and defeat key-absence isolation.
			env.Setenv("KEYRING_DISABLE", "1")

			// Write and index the fixture in $WORK/fixture. Each script indexes its
			// own copy: an index records the directory it was built in, and
			// snipe refuses one that has moved (sn-r1do.4), so a prebuilt index
			// cannot be copied in. Indexing here, outside the script, keeps `go`
			// off the script PATH; the fixture indexes in well under a second.
			dst := filepath.Join(env.WorkDir, "fixture")
			if err := writeScriptFixture(dst); err != nil {
				return fmt.Errorf("write fixture: %w", err)
			}
			index := exec.Command(binPath, "index", dst, "--enrich=false", "--embed-mode=off")
			index.Dir = dst
			// Seal this subprocess boundary too: without KEYRING_DISABLE a real
			// snipe/voyage keychain item (or a locked-keychain prompt) on a
			// developer Mac could leak into the fixture index run.
			index.Env = snipeEnv()
			var iout, ierr bytes.Buffer
			index.Stdout = &iout
			index.Stderr = &ierr
			if err := index.Run(); err != nil {
				return fmt.Errorf("index fixture: %w\n%s\n%s", err, iout.String(), ierr.String())
			}
			env.Setenv("FIXTURE", dst)
			return nil
		},
	})
}

// cmpJSON is `cmp` for two --format json envelopes, ignoring meta.ms (wall
// time, the one field that differs between two runs of the same query). It
// proves an alias and its new form print the same envelope.
func cmpJSON(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! cmpjson")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: cmpjson file1 file2")
	}
	norm := func(name string) string {
		var v map[string]any
		ts.Check(json.Unmarshal([]byte(ts.ReadFile(name)), &v))
		if meta, ok := v["meta"].(map[string]any); ok {
			delete(meta, "ms")
		}
		out, err := json.MarshalIndent(v, "", "  ")
		ts.Check(err)
		return string(out)
	}
	if a, b := norm(args[0]), norm(args[1]); a != b {
		ts.Fatalf("%s and %s differ:\n%s\n---\n%s", args[0], args[1], a, b)
	}
}

// seedEmbed is `seedembed Name=x,y ...`: it stores a fixed embedding for each
// named function in the fixture index, since the fixture indexes with
// --embed-mode=off and no Voyage key. sim --pairs scripts need vectors to score.
func seedEmbed(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: seedembed Name=x,y ...")
	}
	st, err := store.Open(filepath.Join(ts.Getenv("FIXTURE"), ".snipe", "index.db"))
	ts.Check(err)
	defer st.Close()
	for _, a := range args {
		name, vec, ok := strings.Cut(a, "=")
		if !ok {
			ts.Fatalf("bad arg %q", a)
		}
		var emb []float32
		for _, f := range strings.Split(vec, ",") {
			v, perr := strconv.ParseFloat(f, 32)
			ts.Check(perr)
			emb = append(emb, float32(v))
		}
		var id string
		ts.Check(st.DB().QueryRow(`SELECT id FROM symbols WHERE name = ? AND kind = 'func'`, name).Scan(&id))
		ts.Check(st.SaveEmbedding(id, emb, "test"))
	}
}

// jsonSubset proves one --format json envelope's results hold every field of
// another's: each key under results[i] of file1, at any depth, is in file2
// with the same value (arrays compare whole). It proves a deeper depth keeps
// every field of a shallower one (sn-lvub.10). ! jsonsubset proves a field is
// missing or differs.
func jsonSubset(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 2 {
		ts.Fatalf("usage: jsonsubset small.json big.json")
	}
	results := func(name string) []any {
		var v struct {
			Results []any `json:"results"`
		}
		ts.Check(json.Unmarshal([]byte(ts.ReadFile(name)), &v))
		return v.Results
	}
	small, big := results(args[0]), results(args[1])
	missing := ""
	if len(small) == 0 || len(small) != len(big) {
		missing = fmt.Sprintf("results: %d vs %d", len(small), len(big))
	}
	for i := 0; missing == "" && i < len(small); i++ {
		missing = subsetDiff(fmt.Sprintf("results[%d]", i), small[i], big[i])
	}
	switch {
	case neg && missing == "":
		ts.Fatalf("%s holds every field of %s", args[1], args[0])
	case !neg && missing != "":
		ts.Fatalf("%s lacks %s's field %s", args[1], args[0], missing)
	}
}

// subsetDiff returns the path of the first field of small that big lacks or
// holds with another value, or "".
func subsetDiff(path string, small, big any) string {
	sm, ok := small.(map[string]any)
	if !ok {
		if reflect.DeepEqual(small, big) {
			return ""
		}
		return path
	}
	bm, ok := big.(map[string]any)
	if !ok {
		return path
	}
	keys := make([]string, 0, len(sm))
	for k := range sm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		bv, ok := bm[k]
		if !ok {
			return path + "." + k
		}
		if d := subsetDiff(path+"."+k, sm[k], bv); d != "" {
			return d
		}
	}
	return ""
}

// lineSubset proves every line of file1 appears in file2, in the same order
// (file2 may add lines between them). It is jsonsubset for concise output.
func lineSubset(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) != 2 {
		ts.Fatalf("usage: linesubset small.txt big.txt")
	}
	small := strings.Split(strings.TrimRight(ts.ReadFile(args[0]), "\n"), "\n")
	big := strings.Split(strings.TrimRight(ts.ReadFile(args[1]), "\n"), "\n")
	j, missing := 0, ""
	for _, line := range small {
		for j < len(big) && big[j] != line {
			j++
		}
		if j == len(big) {
			missing = line
			break
		}
		j++
	}
	switch {
	case neg && missing == "":
		ts.Fatalf("%s holds every line of %s", args[1], args[0])
	case !neg && missing != "":
		ts.Fatalf("%s lacks %s's line %q (or has it out of order)", args[1], args[0], missing)
	}
}

// writeScriptFixture writes a minimal Go module exercising the features the
// scripted commands assert against:
//   - cross-package refs (alpha ← beta, main → alpha/beta) for `boundary`/`guard`
//   - a named string constant for `trace`/`lits`
//   - an unreferenced exported symbol for `deadcode`
//   - a small call graph for `metrics`/`orient`/`callers`/`callees`/`plan`
//   - a self-contained `shape` package (interface + struct + implementers) so
//     `impl`/`types` resolve real happy paths. shape imports nothing from
//     alpha/beta and is imported by nothing, so it can't perturb the boundary,
//     deadcode-anchor, or call-graph assertions the other scripts rely on.
func writeScriptFixture(dir string) error {
	files := map[string]string{
		"go.mod": "module example.com/fixture\n\ngo 1.20\n",
		"shape/shape.go": `package shape

// Sizer is an interface — impl resolves its implementers.
type Sizer interface {
	Size() int
}

// Box is a struct implementing Sizer — types/lifecycle read its shape.
type Box struct {
	Width  int
	Height int
}

// Size returns the box area.
func (b Box) Size() int {
	return b.Width * b.Height
}

// Dot is a second Sizer implementer.
type Dot struct{}

// Size is always zero.
func (Dot) Size() int {
	return 0
}
`,
		"main.go": `package fixture

import (
	"example.com/fixture/alpha"
	"example.com/fixture/beta"
)

// Run wires the two packages together so refs cross the package boundary.
func Run() string {
	return alpha.Greet() + beta.Reply()
}

// Orphan is exported but never referenced — deadcode should report it.
func Orphan() string {
	return "unused"
}
`,
		"alpha/alpha.go": `package alpha

// Banner is a named string constant — trace/lits index it by value.
const Banner = "banner-token-literal"

// Greet returns the banner.
func Greet() string {
	return Banner
}
`,
		"beta/beta.go": `package beta

import "example.com/fixture/alpha"

// Reply calls across into the alpha package.
func Reply() string {
	return alpha.Greet()
}
`,
		"beta/env.go": `package beta

import "os"

// envToken reads an env var, which lits finds by its name.
func envToken() string {
	return os.Getenv("FIXTURE_ENV_TOKEN")
}
`,
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TestExitCodeContract asserts the exact process exit codes snipe promises, so
// agents chaining commands can gate on them (AXI #6). testscript's `! exec` only
// distinguishes zero from non-zero; this drives the built binary directly to
// pin the specific codes:
//
//	0 — success, or a valid empty answer (no results is not a failure)
//	1 — an error envelope was emitted (e.g. symbol not found)
//	2 — a flag / usage error (deliberate, not kong's default)
func TestExitCodeContract(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "snipe")
	build := exec.Command("go", "build", "-o", binPath, "./")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build snipe: %v\n%s", err, out)
	}

	fixtureDir := t.TempDir()
	if err := writeScriptFixture(fixtureDir); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	index := exec.Command(binPath, "index", fixtureDir, "--enrich=false", "--embed-mode=off")
	index.Dir = fixtureDir
	index.Env = snipeEnv()
	if out, err := index.CombinedOutput(); err != nil {
		t.Fatalf("index fixture: %v\n%s", err, out)
	}

	run := func(args ...string) int {
		cmd := exec.Command(binPath, args...)
		cmd.Dir = fixtureDir
		cmd.Env = snipeEnv()
		if err := cmd.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return ee.ExitCode()
			}
			t.Fatalf("run %v: %v", args, err)
		}
		return 0
	}

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"def", "Run"}, 0},
		{"valid empty (no callers)", []string{"callers", "Orphan"}, 0},
		{"symbol not found", []string{"def", "ZZZNotARealSymbol"}, 1},
		{"unknown bare command", []string{"frobnicatezzz"}, 1},
		{"bad flag", []string{"def", "--definitely-not-a-flag"}, 2},
		// Usage errors found in Run, not by kong, still exit 2.
		{"pack depth arity", []string{"pack", "--depth", "sym", "Greet", "Run"}, 2},
		{"deps mode flag plus positional", []string{"deps", "--importers", "example.com/fixture/alpha", "extra"}, 2},
		{"deps empty mode value", []string{"deps", "--file", ""}, 2},
		{"explain flag at pack depth", []string{"pack", "Greet", "--mode", "deep"}, 2},
		{"usage error as json", []string{"pack", "--depth", "explain", "Greet", "Run", "--format", "json"}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := run(tt.args...); got != tt.want {
				t.Errorf("snipe %v exit = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root from %s", wd)
		}
		dir = parent
	}
}

// TestMissingArgument_IsInvalidArgs runs every command that needs a symbol
// (or package sets) with none. Each must return INVALID_ARGS routed to its
// own --help, not INTERNAL_ERROR routed to doctor (sn-r1do.5): telemetry
// counted these as internal errors, and the doctor hint sent agents to the
// wrong fix.
func TestMissingArgument_IsInvalidArgs(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	binPath := filepath.Join(t.TempDir(), "snipe")
	build := exec.Command("go", "build", "-o", binPath, "./")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build snipe: %v\n%s", err, out)
	}
	fixtureDir := t.TempDir()
	if err := writeScriptFixture(fixtureDir); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	env := append(snipeEnv(), "SNIPE_NO_TELEMETRY=1")
	index := exec.Command(binPath, "index", fixtureDir, "--enrich=false", "--embed-mode=off")
	index.Dir = fixtureDir
	index.Env = env
	if out, err := index.CombinedOutput(); err != nil {
		t.Fatalf("index fixture: %v\n%s", err, out)
	}

	commands := []string{
		"def", "refs", "callers", "callees", "impact", "tests", "impl", "types",
		"sym", "pack", "explain", "lifecycle", "boundary", "sim",
	}
	for _, name := range commands {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(binPath, "--format", "json", name)
			cmd.Dir = fixtureDir
			cmd.Env = env
			out, _ := cmd.Output()
			var resp struct {
				Error struct {
					Code string `json:"code"`
					Next struct {
						Command string `json:"command"`
					} `json:"next"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out, &resp); err != nil {
				t.Fatalf("snipe %s: not a JSON envelope: %v\n%s", name, err, out)
			}
			if resp.Error.Code != "INVALID_ARGS" {
				t.Errorf("snipe %s with no argument: code = %q, want INVALID_ARGS", name, resp.Error.Code)
			}
			if want := "snipe " + name + " --help"; resp.Error.Next.Command != want {
				t.Errorf("snipe %s with no argument: next = %q, want %q", name, resp.Error.Next.Command, want)
			}
		})
	}
}

// snipeEnv is os.Environ() without any VOYAGE_* var, plus KEYRING_DISABLE, so
// the binary under test never reaches the real Voyage API with a developer's
// key or keychain item (sn-pln6).
func snipeEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "VOYAGE_") {
			env = append(env, e)
		}
	}
	return append(env, "KEYRING_DISABLE=1")
}
