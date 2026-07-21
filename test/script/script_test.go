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
//   - risk / index: need a git work tree / would reindex per script (defeats the
//     copy-a-prebuilt-index design) → --help fast-exit, like watch.
//   - imports: a FILE-argument command; file lookups match the absolute path
//     stored at index time, which no longer exists once the index is copied to a
//     fresh $WORK path (name/package queries stay portable, file-path ones do
//     not) → --help fast-exit (real path is blackbox TestImports).
//   - verify / tests / show: the fixture has no diff / no _test.go / stable hex
//     ID to assert → graceful degrade path (JSON contract is in blackbox).
//
// Pattern: build-and-exec. cmd.Execute() calls os.Exit internally (kong's
// FatalIfErrorf), so the binary cannot be driven in-process. Instead the suite
// builds snipe once, indexes a small Go fixture once, and for each script copies
// the indexed fixture into $WORK and prepends the binary's directory to PATH so
// scripts can `exec snipe ...` against a ready-to-query index. No build tag — it
// is its own package, so plain `make` (go test ./...) runs it.
package script

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
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

	// Build + index the fixture once. The .snipe index is path-portable, so we
	// copy the whole indexed tree into each script's $WORK during Setup. This
	// keeps `go` off the script PATH and makes scripts fast (no per-script
	// indexing).
	fixtureDir := t.TempDir()
	if err := writeScriptFixture(fixtureDir); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	index := exec.Command(binPath, "index", fixtureDir, "--enrich=false", "--embed-mode=off")
	index.Dir = fixtureDir
	// Seal this subprocess boundary too: without KEYRING_DISABLE a real
	// snipe/voyage keychain item (or a locked-keychain prompt) on a developer
	// Mac could leak into the fixture index run.
	index.Env = append(os.Environ(), "KEYRING_DISABLE=1")
	var iout, ierr bytes.Buffer
	index.Stdout = &iout
	index.Stderr = &ierr
	if err := index.Run(); err != nil {
		t.Fatalf("index fixture: %v\n%s\n%s", err, iout.String(), ierr.String())
	}

	testscript.Run(t, testscript.Params{
		Dir: filepath.Join("testdata", "script"),
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

			// Copy the pre-indexed fixture into $WORK/fixture. snipe resolves the
			// index root to the go.mod root (D3), and the .snipe index is
			// path-portable, so the copy is immediately queryable.
			dst := filepath.Join(env.WorkDir, "fixture")
			if err := copyTree(fixtureDir, dst); err != nil {
				return err
			}
			env.Setenv("FIXTURE", dst)
			return nil
		},
	})
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

// copyTree recursively copies src into dst.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
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
