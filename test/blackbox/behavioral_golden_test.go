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
