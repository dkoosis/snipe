//go:build blackbox

package blackbox

import (
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

	stdout, _, exitCode := runRaw(t, repoDir, "diagram", "flow", "CallMany")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenDiagram(t, repoDir, stdout)
}

func TestGolden_Diagram_Lifecycle(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, _, exitCode := runRaw(t, repoDir, "diagram", "lifecycle", "Widget")
	if exitCode != 0 {
		t.Fatalf("exit %d", exitCode)
	}
	assertGoldenDiagram(t, repoDir, stdout)
}
