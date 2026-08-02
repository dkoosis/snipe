//go:build blackbox

package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Behavioral golden tests — pin actual command output against a fixed fixture.
// Regenerate: UPDATE_GOLDENS=1 go test -tags blackbox ./test/blackbox/ -run TestGolden
// Then review testdata/golden/*.json before committing.

func TestGolden_Def_Function(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "def", "Callee")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Def_Struct(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "def", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Def_Interface(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "def", "Greeter")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Callers(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "callers", "Callee")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Callees(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "callees", "CallMany")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Refs(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "refs", "Callee")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Impl(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "impl", "Greeter")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Pack_Struct(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "pack", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Types(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "types", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Tests(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "tests", "Callee")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Impact(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "impact", "Callee")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Lifecycle_AllRoles(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "lifecycle", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Lifecycle_IncludeTests(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "lifecycle", "Widget", "--include-tests")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Deps(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "deps", ".")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Pkg(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "pkg", ".")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Context_Boot(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "context", repoDir)
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Context_Conventions(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "context", "--conventions", repoDir)
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenJSON(t, repoDir, stdout)
}

func TestGolden_Diagram_Arch(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// sn-l1kh: the truly-unset default now writes docs/diagrams/arch.md
	// instead of printing D2 to stdout (see cmd/diagram_test.go for that
	// path). --format d2 keeps the explicit-opt-in stdout dump this golden
	// checks byte-for-byte.
	stdout, _, exitCode := runRaw(t, repoDir, "diagram", "arch", "--format", "d2")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenDiagram(t, repoDir, stdout)
}

func TestGolden_Diagram_Flow(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// sn-l1kh.8: the truly-unset default now writes
	// docs/diagrams/flow-<name>.md instead of printing D2 to stdout (see
	// TestGolden_Diagram_Flow_DefaultWritesDoc below for that path).
	// --format d2 keeps the explicit-opt-in stdout dump this golden checks
	// byte-for-byte.
	stdout, _, exitCode := runRaw(t, repoDir, "diagram", "flow", "CallMany", "--format", "d2")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenDiagram(t, repoDir, stdout)
}

func TestGolden_Diagram_Lifecycle(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	// sn-l1kh.8: the truly-unset default now writes
	// docs/diagrams/lifecycle-<name>.md instead of printing D2 to stdout (see
	// TestGolden_Diagram_Lifecycle_DefaultWritesDoc and
	// TestGolden_Diagram_Lifecycle_QualifiedNameUsesResolvedSymbol below for
	// that path). --format d2 keeps the explicit-opt-in stdout dump this
	// golden checks byte-for-byte.
	stdout, _, exitCode := runRaw(t, repoDir, "diagram", "lifecycle", "Widget", "--format", "d2")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenDiagram(t, repoDir, stdout)
}

// TestGolden_Diagram_Flow_DefaultWritesDoc pins the new emitDoc default path
// (sn-l1kh.8): no --format ⇒ docs/diagrams/flow-<entry>.md on disk, nothing
// on stdout, mirroring arch's default (sn-l1kh.1).
func TestGolden_Diagram_Flow_DefaultWritesDoc(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "diagram", "flow", "CallMany")
	if exitCode != 0 {
		t.Fatalf("exit %d stderr=%s", exitCode, string(stderr))
	}
	// Default format writes the doc to disk and prints only a "wrote <path>"
	// confirmation — never the D2 source itself.
	if strings.Contains(string(stdout), "```d2") {
		t.Errorf("default format must not dump D2 to stdout; got %q", string(stdout))
	}

	docPath := filepath.Join(repoDir, "docs", "diagrams", "flow-CallMany.md")
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	for _, want := range []string{"diagram flow · entry=CallMany", "```d2"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("doc missing %q:\n%s", want, doc)
		}
	}
}

// TestGolden_Diagram_Lifecycle_DefaultWritesDoc mirrors the flow case above
// for `diagram lifecycle` (sn-l1kh.8).
func TestGolden_Diagram_Lifecycle_DefaultWritesDoc(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "diagram", "lifecycle", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d stderr=%s", exitCode, string(stderr))
	}
	// Default format writes the doc to disk and prints only a "wrote <path>"
	// confirmation — never the D2 source itself.
	if strings.Contains(string(stdout), "```d2") {
		t.Errorf("default format must not dump D2 to stdout; got %q", string(stdout))
	}

	docPath := filepath.Join(repoDir, "docs", "diagrams", "lifecycle-Widget.md")
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	for _, want := range []string{"diagram lifecycle · type=Widget", "```d2"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("doc missing %q:\n%s", want, doc)
		}
	}
}

// TestGolden_Diagram_Lifecycle_QualifiedNameUsesResolvedSymbol is the
// qualified-name-safety acceptance case (sn-l1kh.8 plan step 2):
// runDiagramLifecycle must name the doc from the RESOLVED sym.Name ("Widget"),
// not the raw qualified CLI arg ("example.com/lifecyclefix/store.Widget"),
// which contains "/" and "." — both sanitized to "_" by diagram.SanitizeID,
// but the resolved name is the canonical, predictable filename and the only
// value that keeps repeated flow/lifecycle diagrams for the same symbol
// landing on the same doc regardless of how the caller spelled the query.
func TestGolden_Diagram_Lifecycle_QualifiedNameUsesResolvedSymbol(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	_, stderr, exitCode := runRaw(t, repoDir, "diagram", "lifecycle", "example.com/lifecyclefix/store.Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d stderr=%s", exitCode, string(stderr))
	}

	diagramsDir := filepath.Join(repoDir, "docs", "diagrams")
	docPath := filepath.Join(diagramsDir, "lifecycle-Widget.md")
	if _, err := os.Stat(docPath); err != nil {
		t.Fatalf("expected resolved-name doc %s: %v", docPath, err)
	}

	entries, err := os.ReadDir(diagramsDir)
	if err != nil {
		t.Fatalf("read %s: %v", diagramsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("unexpected subdirectory %q under docs/diagrams/ (qualified CLI arg must not leak into the path)", e.Name())
		}
	}
}
