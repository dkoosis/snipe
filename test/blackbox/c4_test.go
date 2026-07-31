//go:build blackbox

package blackbox

import (
	"path/filepath"
	"strings"
	"testing"
)

// writeC4Fixture writes a repo shaped for the c4 command's three signal
// kinds: a Go main entry point (container), a package importing a SQLite
// driver (datastore), a package with a fake external SDK import plus an
// os.Getenv env-var call (external system, per the design field's own
// stripe-go/STRIPE_API_KEY example), and a package importing testify only
// from a _test.go file (must NOT surface as external).
func writeC4Fixture(t *testing.T) (repoDir string) {
	t.Helper()

	repoDir = t.TempDir()

	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/c4fixture\n\ngo 1.21\n")
	writeFile(t, filepath.Join(repoDir, "main.go"), `package c4fixture

func main() {}
`)

	writeFile(t, filepath.Join(repoDir, "internal/store/db.go"), `package store

import _ "modernc.org/sqlite"

func Open() {}
`)

	writeFile(t, filepath.Join(repoDir, "billing/pay.go"), `package billing

import (
	"os"

	"github.com/stripe/stripe-go/v72"
)

// Charge is a fake charge call exercising both external-system detection
// paths: an env-var literal and a third-party SDK import.
func Charge() string {
	os.Getenv("STRIPE_API_KEY")
	return stripe.Key
}
`)

	writeFile(t, filepath.Join(repoDir, "internal/util/assert_helper.go"), `package util

func Helper() string { return "ok" }
`)
	writeFile(t, filepath.Join(repoDir, "internal/util/assert_helper_test.go"), `package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHelper(t *testing.T) {
	assert.Equal(t, "ok", Helper())
}
`)

	return repoDir
}

func TestC4_ContainerLevel_JSON(t *testing.T) {
	repoDir := writeC4Fixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "c4")
	if exitCode != 0 {
		t.Fatalf("c4 exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}

	resp := assertEnvelope(t, stdout, "c4")
	assertResponseContract(t, resp, responseExpectations{command: "c4", requireRepoRoot: true, requireIndexState: true})

	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := requireMap(t, results[0], "results[0]")

	// Container: the main package, named after the module.
	containers := requireSlice(t, r["containers"], "containers")
	if len(containers) != 1 {
		t.Fatalf("want 1 container, got %d: %v", len(containers), containers)
	}
	c := requireMap(t, containers[0], "containers[0]")
	if name := getString(t, c["name"], "containers[0].name"); name != "c4fixture" {
		t.Errorf("want container name c4fixture, got %q", name)
	}

	// Datastore: SQLite via modernc.org/sqlite.
	datastores := requireSlice(t, r["datastores"], "datastores")
	if len(datastores) != 1 {
		t.Fatalf("want 1 datastore, got %d: %v", len(datastores), datastores)
	}
	d := requireMap(t, datastores[0], "datastores[0]")
	if name := getString(t, d["name"], "datastores[0].name"); name != "SQLite" {
		t.Errorf("want datastore SQLite, got %q", name)
	}

	// External: stripe-go import + STRIPE_API_KEY env merge into ONE system
	// with file:line evidence (AC #3).
	external := requireSlice(t, r["external"], "external")
	if len(external) != 1 {
		t.Fatalf("want 1 external system (env+import merged), got %d: %v", len(external), external)
	}
	e := requireMap(t, external[0], "external[0]")
	if name := getString(t, e["name"], "external[0].name"); name != "STRIPE" {
		t.Errorf("want external system STRIPE, got %q", name)
	}
	evidence := requireSlice(t, e["evidence"], "external[0].evidence")
	if len(evidence) != 2 {
		t.Errorf("want 2 evidence entries (env + import), got %d: %v", len(evidence), evidence)
	}
	file := getString(t, e["file"], "external[0].file")
	if !strings.HasSuffix(file, "billing/pay.go") {
		t.Errorf("want evidence file in billing/pay.go, got %q", file)
	}
	line := getFloat(t, e["line"], "external[0].line")
	if line <= 0 {
		t.Errorf("want a positive line number, got %v", line)
	}

	// testify must never surface as an external system, merged or otherwise.
	for _, item := range external {
		m := requireMap(t, item, "external[i]")
		name := getString(t, m["name"], "external[i].name")
		if strings.Contains(strings.ToLower(name), "testify") {
			t.Errorf("testify must not appear as an external system, got %v", external)
		}
	}
}

func TestC4_ResultsNeverNull(t *testing.T) {
	// A repo with none of the three signal kinds still returns a non-null
	// results array (AC #2), just with empty sections.
	repoDir := t.TempDir()
	writeFile(t, filepath.Join(repoDir, "go.mod"), "module example.com/empty\n\ngo 1.21\n")
	writeFile(t, filepath.Join(repoDir, "main.go"), "package empty\n\nfunc main() {}\n")
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "c4")
	if exitCode != 0 {
		t.Fatalf("c4 exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	resp := assertEnvelope(t, stdout, "c4")
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := requireMap(t, results[0], "results[0]")
	// datastores/external/flows may be omitted (omitempty) when empty, but
	// they must never be present-and-null.
	for _, field := range []string{"datastores", "external", "flows", "components"} {
		if v, ok := r[field]; ok && v == nil {
			t.Errorf("field %q is present but null; want omitted or a non-null array", field)
		}
	}
}

func TestC4_LevelContext_NoContainers(t *testing.T) {
	repoDir := writeC4Fixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "c4", "--level", "context")
	if exitCode != 0 {
		t.Fatalf("c4 --level context exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	resp := assertEnvelope(t, stdout, "c4")
	results := requireSlice(t, resp["results"], "results")
	r := requireMap(t, results[0], "results[0]")
	if _, ok := r["containers"]; ok {
		t.Errorf("--level context must omit containers, got %v", r["containers"])
	}
}

func TestC4_TextOutput(t *testing.T) {
	repoDir := writeC4Fixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "c4")
	if exitCode != 0 {
		t.Fatalf("c4 exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
	text := string(stdout)
	if !strings.Contains(text, "## containers") {
		t.Errorf("want a containers section, got:\n%s", text)
	}
	if !strings.Contains(text, "SQLite") {
		t.Errorf("want SQLite datastore in text output, got:\n%s", text)
	}
	if !strings.Contains(text, "STRIPE") {
		t.Errorf("want STRIPE external system in text output, got:\n%s", text)
	}
	if strings.Contains(text, "testify") {
		t.Errorf("testify must not appear in text output, got:\n%s", text)
	}
}
