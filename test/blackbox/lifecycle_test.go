//go:build blackbox

package blackbox

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// writeLifecycleFixture creates a repo with a Widget type that has all four
// CRUD roles exercised across packages, plus a test-file reference to verify
// the Tests bucket.
func writeLifecycleFixture(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	writeFile(t, filepath.Join(repoDir, "go.mod"),
		"module example.com/lifecyclefix\n\ngo 1.20\n")

	// store package: defines Widget and CRUD operations on it.
	storeContent := `package store

// Widget is the type under lifecycle trace.
type Widget struct {
	ID   string
	Name string
}

// NewWidget constructs a Widget. R3: create name + signature returns *Widget.
func NewWidget(name string) *Widget {
	return &Widget{Name: name}
}

// UpdateWidget mutates a Widget in place. R5: mutate verb + takes *Widget.
func UpdateWidget(w *Widget, name string) {
	w.Name = name
}

// DeleteWidget removes a widget by id. R4: name prefix Delete.
func DeleteWidget(w *Widget) {
	_ = w
}

// ReadWidget returns the widget name. R7 default: no create/mutate/delete signal.
func ReadWidget(w *Widget) string {
	return w.Name
}
`
	writeFile(t, filepath.Join(repoDir, "store", "store.go"), storeContent)

	// consumer package: constructs a Widget via struct literal. R2.
	consumerContent := `package consumer

import "example.com/lifecyclefix/store"

// BuildWidget uses a struct literal. R2 snippet rule should fire Create.
func BuildWidget() *store.Widget {
	return &store.Widget{Name: "x"}
}
`
	writeFile(t, filepath.Join(repoDir, "consumer", "consumer.go"), consumerContent)

	// test file — should land in the Tests bucket by default.
	testContent := `package store

import "testing"

func TestWidgetRoundtrip(t *testing.T) {
	// Direct Widget reference so the lifecycle trace has a test-file ref
	// that can be bucketed under Tests.
	w := &Widget{Name: "t"}
	UpdateWidget(w, "u")
	if ReadWidget(w) != "u" {
		t.Fatal("bad read")
	}
}
`
	writeFile(t, filepath.Join(repoDir, "store", "store_test.go"), testContent)

	return repoDir
}

func TestLifecycle_AllRolesPopulated_When_FixtureHasCRUD(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "lifecycle", "Widget")
	if exitCode != 0 {
		t.Fatalf("lifecycle exit %d stderr=%s stdout=%s",
			exitCode, string(stderr), string(stdout))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "lifecycle",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	result := requireMap(t, results[0], "results[0]")

	if result["type"] != "Widget" {
		t.Fatalf("type = %v, want Widget", result["type"])
	}

	groups := requireSlice(t, result["groups"], "groups")

	// Collect group -> [func names] for assertions.
	byRole := map[string][]string{}
	byRoleFiles := map[string][]string{}
	for _, g := range groups {
		group := requireMap(t, g, "group")
		role, _ := group["role"].(string)
		funcs, _ := group["funcs"].([]any)
		for _, fn := range funcs {
			f := requireMap(t, fn, "func")
			name, _ := f["name"].(string)
			file, _ := f["file"].(string)
			byRole[role] = append(byRole[role], name)
			byRoleFiles[role] = append(byRoleFiles[role], file)
		}
	}

	// All four CRUD groups must have at least one classified function.
	for _, role := range []string{"Create", "Mutate", "Read", "Delete"} {
		if len(byRole[role]) == 0 {
			t.Errorf("%s bucket empty; byRole=%v", role, byRole)
		}
	}

	// Specific expected classifications.
	wantNames := map[string]string{
		"NewWidget":    "Create",
		"BuildWidget":  "Create",
		"UpdateWidget": "Mutate",
		"DeleteWidget": "Delete",
		"ReadWidget":   "Read",
	}
	for name, wantRole := range wantNames {
		found := false
		for _, n := range byRole[wantRole] {
			if n == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in %s bucket; got %v", name, wantRole, byRole[wantRole])
		}
	}

	// Test-file references default to a separate Tests bucket.
	if len(byRole["Tests"]) == 0 {
		t.Errorf("Tests bucket should contain the _test.go reference; byRole=%v", byRole)
	}
	for _, file := range byRoleFiles["Tests"] {
		if !strings.HasSuffix(file, "_test.go") {
			t.Errorf("Tests bucket file %q missing _test.go suffix", file)
		}
	}

	// Non-test buckets must not contain _test.go files.
	for role, files := range byRoleFiles {
		if role == "Tests" {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				t.Errorf("role %s contains test file %q", role, f)
			}
		}
	}
}

func TestLifecycle_StableOrdering_When_RunTwice(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	run1, _, ec1 := run(t, repoDir, "lifecycle", "Widget")
	run2, _, ec2 := run(t, repoDir, "lifecycle", "Widget")
	if ec1 != 0 || ec2 != 0 {
		t.Fatalf("lifecycle non-zero exit: %d %d", ec1, ec2)
	}

	// Compare the results[0].groups -> funcs ordering across runs. Meta has
	// ms timing that will differ between runs, so strip it.
	names := func(data []byte) [][]string {
		var resp map[string]any
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		results, _ := resp["results"].([]any)
		result, _ := results[0].(map[string]any)
		groups, _ := result["groups"].([]any)
		out := [][]string{}
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			funcs, _ := gm["funcs"].([]any)
			row := []string{gm["role"].(string)}
			for _, fn := range funcs {
				fm, _ := fn.(map[string]any)
				row = append(row, fm["file"].(string)+":"+fm["name"].(string))
			}
			out = append(out, row)
		}
		return out
	}

	a := names(run1)
	b := names(run2)
	if len(a) != len(b) {
		t.Fatalf("group count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			t.Fatalf("group %d size differs: %v vs %v", i, a[i], b[i])
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("group %d pos %d differs: %q vs %q", i, j, a[i][j], b[i][j])
			}
		}
	}
}

func TestLifecycle_IncludeTests_MergesTestsIntoRoles(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "lifecycle", "Widget", "--include-tests")
	if exitCode != 0 {
		t.Fatalf("lifecycle --include-tests exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	result := requireMap(t, results[0], "results[0]")
	groups := requireSlice(t, result["groups"], "groups")

	for _, g := range groups {
		group := requireMap(t, g, "group")
		role, _ := group["role"].(string)
		if role == "Tests" {
			t.Fatalf("Tests bucket should be absent with --include-tests; got %v", group)
		}
	}
}

func TestLifecycle_TokenBudget_Respected_When_MaxTokensLow(t *testing.T) {
	repoDir := writeLifecycleFixture(t)
	indexRepo(t, repoDir)

	// Text output; tiny budget. Formatter must truncate, not blow the budget.
	stdout, stderr, exitCode := runRaw(t, repoDir,
		"lifecycle", "Widget", "--max-tokens", "120")
	if exitCode != 0 {
		t.Fatalf("lifecycle --max-tokens exit %d stderr=%s", exitCode, string(stderr))
	}

	// Rough 4-chars-per-token estimate; output must not exceed ~budget with
	// headroom. A full run of this fixture emits well over 500 tokens, so a
	// 120-token cap means truncation kicked in.
	approxTokens := len(stdout) / 4
	if approxTokens > 400 {
		t.Fatalf("token budget not respected: got ~%d tokens, want <=400 for --max-tokens=120\noutput:\n%s",
			approxTokens, string(stdout))
	}
	if len(stdout) == 0 {
		t.Fatal("no output under --max-tokens")
	}
}
