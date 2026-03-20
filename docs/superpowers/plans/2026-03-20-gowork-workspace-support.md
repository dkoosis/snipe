# go.work Workspace Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `snipe index` discover and index all modules in a `go.work` workspace, not just the root module.

**Architecture:** Parse `go.work` with `golang.org/x/mod/modfile.ParseWork`, generate one `<dir>/...` load pattern per `use` directive, and feed those patterns to the existing `go/packages.Load` pipeline. No changes to storage, queries, or downstream commands.

**Tech Stack:** `golang.org/x/mod/modfile` (already an indirect dependency), Go standard `os` for file detection.

**Spec:** `docs/superpowers/specs/2026-03-20-gowork-workspace-support-design.md`

**Verify:** `mage` (build + lint + test). Blackbox tests: `mage qa` or `go test -tags blackbox ./test/blackbox/...`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/index/workspace.go` | New — `WorkspacePatterns(dir) ([]string, error)`: detect go.work, parse, return patterns |
| `internal/index/workspace_test.go` | New — unit tests for WorkspacePatterns |
| `internal/index/loader.go` | Modify — default pattern fallback uses WorkspacePatterns |
| `cmd/index.go` | Modify — primary call site uses WorkspacePatterns |
| `test/blackbox/fixture_test.go` | Modify — add `writeWorkspaceFixture` helper |
| `test/blackbox/cli_workflows_test.go` | Modify — add workspace indexing + def test |

---

### Task 1: WorkspacePatterns — failing tests

**Files:**
- Create: `internal/index/workspace_test.go`
- Create: `internal/index/workspace.go` (stub)

- [ ] **Step 1: Write the test file**

```go
package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspacePatterns_WithGoWork_ReturnsPerModulePatterns(t *testing.T) {
	dir := t.TempDir()
	goWork := `go 1.21

use (
	.
	./graph
	./store
	./mcp
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		"./...":       true,
		"./graph/...": true,
		"./store/...": true,
		"./mcp/...":   true,
	}
	if len(patterns) != len(want) {
		t.Fatalf("got %d patterns, want %d: %v", len(patterns), len(want), patterns)
	}
	for _, p := range patterns {
		if !want[p] {
			t.Errorf("unexpected pattern: %s", p)
		}
	}
}

func TestWorkspacePatterns_NoGoWork_ReturnsDotSlashDotDotDot(t *testing.T) {
	dir := t.TempDir()

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./..." {
		t.Fatalf("got %v, want [./...]", patterns)
	}
}

func TestWorkspacePatterns_MalformedGoWork_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte("not a valid go.work file {{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := WorkspacePatterns(dir)
	if err == nil {
		t.Fatal("expected error for malformed go.work")
	}
}

func TestWorkspacePatterns_EmptyUseDirectives_ReturnsDotSlashDotDotDot(t *testing.T) {
	dir := t.TempDir()
	goWork := "go 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./..." {
		t.Fatalf("got %v, want [./...]", patterns)
	}
}

func TestWorkspacePatterns_NonexistentUseDir_StillReturnsPattern(t *testing.T) {
	dir := t.TempDir()
	goWork := "go 1.21\n\nuse ./doesnotexist\n"
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(goWork), 0o644); err != nil {
		t.Fatal(err)
	}

	patterns, err := WorkspacePatterns(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(patterns) != 1 || patterns[0] != "./doesnotexist/..." {
		t.Fatalf("got %v, want [./doesnotexist/...]", patterns)
	}
}
```

- [ ] **Step 2: Write the stub**

Create `internal/index/workspace.go`:

```go
package index

// WorkspacePatterns returns load patterns for go/packages.
// If go.work exists at dir, returns a pattern per workspace module.
// Otherwise returns ["./..."].
func WorkspacePatterns(dir string) ([]string, error) {
	return []string{"./..."}, nil
}
```

- [ ] **Step 3: Run tests — verify failures**

Run: `go test ./internal/index/ -run TestWorkspacePatterns -v`
Expected: `TestWorkspacePatterns_WithGoWork_ReturnsPerModulePatterns` and `TestWorkspacePatterns_NonexistentUseDir_StillReturnsPattern` FAIL. Other tests PASS (stub returns `["./..."]`).

- [ ] **Step 4: Commit**

```bash
git add internal/index/workspace.go internal/index/workspace_test.go
git commit -m "test: add WorkspacePatterns tests (red)"
```

---

### Task 2: WorkspacePatterns — implementation

**Files:**
- Modify: `internal/index/workspace.go`

- [ ] **Step 1: Implement WorkspacePatterns**

Replace the stub in `internal/index/workspace.go`:

```go
package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// WorkspacePatterns returns load patterns for go/packages.
// If go.work exists at dir, returns a pattern per workspace module.
// Otherwise returns ["./..."].
func WorkspacePatterns(dir string) ([]string, error) {
	goWorkPath := filepath.Join(dir, "go.work")

	data, err := os.ReadFile(goWorkPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{"./..."}, nil
		}
		return nil, fmt.Errorf("read go.work: %w", err)
	}

	wf, err := modfile.ParseWork(goWorkPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.work: %w", err)
	}

	if len(wf.Use) == 0 {
		return []string{"./..."}, nil
	}

	patterns := make([]string, 0, len(wf.Use))
	for _, u := range wf.Use {
		patterns = append(patterns, u.Path+"/...")
	}
	return patterns, nil
}
```

- [ ] **Step 2: Run tests — verify all pass**

Run: `go test ./internal/index/ -run TestWorkspacePatterns -v`
Expected: All 5 tests PASS.

- [ ] **Step 3: Run full suite**

Run: `mage`
Expected: PASS (no regressions).

- [ ] **Step 4: Commit**

```bash
git add internal/index/workspace.go
git commit -m "feat: implement WorkspacePatterns for go.work support (#132)"
```

---

### Task 3: Wire into cmd/index.go (primary call site)

**Files:**
- Modify: `cmd/index.go:135-140`

- [ ] **Step 1: Replace hardcoded pattern**

In `cmd/index.go`, replace lines 135-140:

```go
// OLD:
result, err := index.Load(index.LoadConfig{
    Context:  GetContext(),
    Dir:      absDir,
    Patterns: []string{"./..."},
    Tests:    true,
})

// NEW:
patterns, err := index.WorkspacePatterns(absDir)
if err != nil {
    return fmt.Errorf("resolve workspace patterns: %w", err)
}

result, err := index.Load(index.LoadConfig{
    Context:  GetContext(),
    Dir:      absDir,
    Patterns: patterns,
    Tests:    true,
})
```

- [ ] **Step 2: Run mage**

Run: `mage`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/index.go
git commit -m "feat: use WorkspacePatterns in index command (#132)"
```

---

### Task 4: Wire into loader.go (defensive fallback)

**Files:**
- Modify: `internal/index/loader.go:59-61`

- [ ] **Step 1: Update default pattern resolution**

In `internal/index/loader.go`, replace lines 59-61:

```go
// OLD:
if len(cfg.Patterns) == 0 {
    cfg.Patterns = []string{"./..."}
}

// NEW:
if len(cfg.Patterns) == 0 {
    cfg.Patterns, _ = WorkspacePatterns(cfg.Dir)
    if len(cfg.Patterns) == 0 {
        cfg.Patterns = []string{"./..."}
    }
}
```

- [ ] **Step 2: Run mage**

Run: `mage`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/index/loader.go
git commit -m "feat: loader default pattern resolves workspace modules (#132)"
```

---

### Task 5: Blackbox test — workspace indexing

**Files:**
- Modify: `test/blackbox/fixture_test.go` — add `writeWorkspaceFixture`
- Modify: `test/blackbox/cli_workflows_test.go` — add workspace test

- [ ] **Step 1: Add workspace fixture helper**

Add to `test/blackbox/fixture_test.go`:

```go
// writeWorkspaceFixture creates a multi-module go.work project.
// Returns the repo dir and a map of useful paths.
func writeWorkspaceFixture(t *testing.T) (repoDir string, paths map[string]string) {
	t.Helper()

	repoDir = t.TempDir()
	paths = make(map[string]string)

	// Workspace root
	writeFile(t, filepath.Join(repoDir, "go.work"), `go 1.21

use (
	.
	./lib
)
`)

	// Root module
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/workspace\n\ngo 1.21\n")
	writeFile(t, filepath.Join(repoDir, "main.go"), `package workspace

import "example.com/workspace/lib"

func UseLib() string {
	return lib.Hello()
}
`)
	paths["main"] = filepath.Join(repoDir, "main.go")

	// Lib module (sibling workspace module)
	writeFile(t, filepath.Join(repoDir, "lib", "go.mod"), "module example.com/workspace/lib\n\ngo 1.21\n")
	writeFile(t, filepath.Join(repoDir, "lib", "lib.go"), `package lib

// Hello returns a greeting from the lib module.
func Hello() string {
	return "hello from lib"
}
`)
	paths["lib"] = filepath.Join(repoDir, "lib", "lib.go")

	return repoDir, paths
}
```

- [ ] **Step 2: Add workspace index + def test**

Add to `test/blackbox/cli_workflows_test.go`:

```go
func TestIndex_WorkspaceFixture_IndexesAllModules(t *testing.T) {
	repoDir, _ := writeWorkspaceFixture(t)

	// Index the workspace
	stdout, stderr, exitCode := run(t, repoDir, "index", repoDir)
	if exitCode != 0 {
		t.Fatalf("index exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "index",
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	// Verify lib.Hello is findable (proves lib module was indexed)
	stdout, stderr, exitCode = run(t, repoDir, "def", "Hello")
	if exitCode != 0 {
		t.Fatalf("def exit %d stderr=%s", exitCode, string(stderr))
	}

	defResp := parseJSON(t, stdout)
	results := requireSlice(t, defResp["results"], "results")
	if len(results) == 0 {
		t.Fatal("expected to find Hello in lib module, got 0 results")
	}

	// Verify the result is from the lib module
	first := requireMap(t, results[0], "results[0]")
	file, _ := first["file"].(string)
	if !strings.Contains(file, "lib") {
		t.Errorf("expected Hello from lib module, got file: %s", file)
	}
}
```

- [ ] **Step 3: Run blackbox tests**

Run: `go test -tags blackbox ./test/blackbox/ -run TestIndex_WorkspaceFixture -v -timeout 60s`
Expected: PASS — lib module's `Hello` symbol is found.

- [ ] **Step 4: Run full suite**

Run: `mage`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add test/blackbox/fixture_test.go test/blackbox/cli_workflows_test.go
git commit -m "test: blackbox test for go.work workspace indexing (#132)"
```

---

### Task 6: Run mage qa + manual verification

- [ ] **Step 1: Run mage qa**

Run: `mage qa`
Expected: PASS — all checks including race detector and blackbox tests.

- [ ] **Step 2: Manual verification on trixi**

Run: `cd /Users/vcto/Projects/trixi && /Users/vcto/Projects/snipe/snipe index . 2>&1`
Expected: stderr shows loading 23+ packages (not 2), "Found N symbols" where N is significantly higher than root-module-only count.

- [ ] **Step 3: Verify cross-module lookup on trixi**

Run: `cd /Users/vcto/Projects/trixi && /Users/vcto/Projects/snipe/snipe def Hello` (or any symbol from a non-root module)
Expected: Symbol found in the correct module.

- [ ] **Step 4: Run go mod tidy**

Run: `go mod tidy` (from snipe repo root)
Expected: `golang.org/x/mod` promoted from indirect to direct in go.mod. Commit if changed.

- [ ] **Step 5: Final commit if go.mod changed**

```bash
git add go.mod go.sum
git commit -m "chore: promote golang.org/x/mod to direct dependency"
```
