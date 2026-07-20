// Package script runs testscript (.txtar) smoke tests against the snipe CLI.
//
// These are usage contracts: each script asserts a command's exit code plus one
// stable human-readable line of output. They complement — and do NOT replace —
// the JSON-envelope contract tests in test/blackbox. The blackbox suite proves
// the --format json protocol; this suite proves the default Claude-text CLI
// surface for commands the blackbox suite does not exercise.
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
//   - cross-package refs (alpha ← beta, main → alpha/beta) for `boundary`
//   - a named string constant for `trace`/`lits`
//   - an unreferenced exported symbol for `deadcode`
//   - a small call graph for `metrics`/`orient`
func writeScriptFixture(dir string) error {
	files := map[string]string{
		"go.mod": "module example.com/fixture\n\ngo 1.20\n",
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
