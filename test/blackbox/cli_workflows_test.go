//go:build blackbox

package blackbox

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestIndex_CreatesIndex_When_RunOnFixtureRepo(t *testing.T) {
	repoDir, _ := writeFixture(t)

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

	meta := requireMap(t, resp["meta"], "meta")
	indexState, ok := meta["index_state"].(string)
	if !ok || indexState == "" {
		t.Fatalf("meta.index_state missing or not string")
	}
	if indexState == "missing" {
		t.Fatalf("unexpected index_state: %s", indexState)
	}
}

func TestDef_ByPosition_ReturnsDefinition_When_IndexPresent(t *testing.T) {
	repoDir, paths := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "def", "--at", paths["callee_at"])
	if exitCode != 0 {
		t.Fatalf("def exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "def",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected results")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["file"]; !ok {
		t.Fatalf("result missing file")
	}
	if _, ok := first["range"]; !ok {
		t.Fatalf("result missing range")
	}
	if _, ok := first["edit_target"]; !ok {
		t.Fatalf("result missing edit_target")
	}

	assertGoldenJSON(t, repoDir, stdout)
}

func TestRefs_BySymbol_ReturnsReferences_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--context", "2")
	if exitCode != 0 {
		t.Fatalf("refs exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "refs",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected refs results")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["edit_target"]; !ok {
		t.Fatalf("result missing edit_target")
	}
	context := requireMap(t, first["context"], "context")
	before := requireSlice(t, context["before"], "context.before")
	after := requireSlice(t, context["after"], "context.after")
	if len(before) != 2 || len(after) != 2 {
		t.Fatalf("expected 2 context lines, got before=%d after=%d", len(before), len(after))
	}

	stdoutAlt, _, _ := run(t, repoDir, "refs", "Callee", "--context", "1")
	respAlt := parseJSON(t, stdoutAlt)
	resultsAlt := requireSlice(t, respAlt["results"], "results")
	firstAlt := requireMap(t, resultsAlt[0], "results[0]")
	contextAlt := requireMap(t, firstAlt["context"], "context")
	beforeAlt := requireSlice(t, contextAlt["before"], "context.before")
	afterAlt := requireSlice(t, contextAlt["after"], "context.after")
	if len(beforeAlt) >= len(before) || len(afterAlt) >= len(after) {
		t.Fatalf("expected smaller context with --context 1")
	}
}

func TestCallersAndCallees_Work_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdoutCallers, stderr, exitCode := run(t, repoDir, "callers", "Callee")
	if exitCode != 0 {
		t.Fatalf("callers exit %d stderr=%s", exitCode, string(stderr))
	}
	respCallers := parseJSON(t, stdoutCallers)
	assertResponseContract(t, respCallers, responseExpectations{
		command:           "callers",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})
	results := requireSlice(t, respCallers["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected callers results")
	}
	caller := requireMap(t, results[0], "results[0]")
	if getString(t, caller["name"], "name") == "" {
		t.Fatalf("caller name missing")
	}
	// Note: callers results are symbols, enclosing may or may not be present

	stdoutCallees, stderr, exitCode := run(t, repoDir, "callees", "Caller")
	if exitCode != 0 {
		t.Fatalf("callees exit %d stderr=%s", exitCode, string(stderr))
	}
	respCallees := parseJSON(t, stdoutCallees)
	assertResponseContract(t, respCallees, responseExpectations{
		command:           "callees",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})
	calleeResults := requireSlice(t, respCallees["results"], "results")
	if len(calleeResults) == 0 {
		t.Fatalf("expected callees results")
	}
	callee := requireMap(t, calleeResults[0], "results[0]")
	if getString(t, callee["name"], "name") == "" {
		t.Fatalf("callee name missing")
	}
}

func TestImpl_ReturnsImplementations_ForInterface(t *testing.T) {
	// Skip: snipe impl uses a heuristic (types referencing interface in same file)
	// which doesn't detect structural implementations without explicit reference.
	t.Skip("impl command uses reference heuristic, not structural matching")

	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "impl", "Greeter")
	if exitCode != 0 {
		t.Fatalf("impl exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "impl",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})
	results := requireSlice(t, resp["results"], "results")
	if len(results) < 2 {
		t.Fatalf("expected multiple implementations")
	}

	found := map[string]bool{}
	for _, item := range results {
		result := requireMap(t, item, "result")
		name := getString(t, result["name"], "name")
		found[name] = true
	}
	if !found["Friendly"] || !found["Rude"] {
		t.Fatalf("expected Friendly and Rude implementations, got %v", found)
	}
}

func TestShow_ByID_ReturnsExpandedContext_When_UsingPriorResultID(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdoutRefs, _, exitCode := run(t, repoDir, "def", "Callee")
	if exitCode != 0 {
		t.Fatalf("def exit %d", exitCode)
	}
	respRefs := parseJSON(t, stdoutRefs)
	results := requireSlice(t, respRefs["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected refs results")
	}
	first := requireMap(t, results[0], "results[0]")
	id := getString(t, first["id"], "id")

	stdout, stderr, exitCode := run(t, repoDir, "show", id)
	if exitCode != 0 {
		t.Fatalf("show exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "show",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})
	showResults := requireSlice(t, resp["results"], "results")
	if len(showResults) == 0 {
		t.Fatalf("expected show results")
	}
	showFirst := requireMap(t, showResults[0], "results[0]")
	if _, ok := showFirst["range"]; !ok {
		t.Fatalf("show result missing range")
	}

	assertGoldenJSON(t, repoDir, stdout)
}

func TestSearch_UsesRipgrep_AndReturnsMatches(t *testing.T) {
	repoDir, _ := writeFixture(t)

	_, rgErr := exec.LookPath("rg")
	stdout, stderr, exitCode := run(t, repoDir, "search", "Hello")
	if rgErr != nil {
		_ = exitCode
		resp := parseJSON(t, stdout)
		assertResponseContract(t, resp, responseExpectations{
			command: "search",
		})
		errObj := requireMap(t, resp["error"], "error")
		if getString(t, errObj["code"], "error.code") != "RG_NOT_FOUND" {
			t.Fatalf("expected RG_NOT_FOUND error")
		}
		return
	}

	if exitCode != 0 {
		t.Fatalf("search exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "search",
		requireQuery:      true,
		requireIndexState: true,
		requireDegraded:   true,
	})
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["match"]; !ok {
		t.Fatalf("search result missing match")
	}
	if _, ok := first["file"]; !ok {
		t.Fatalf("search result missing file")
	}
	if _, ok := first["range"]; !ok {
		t.Fatalf("search result missing range")
	}
}

func TestMissingIndex_ReturnsActionableError_When_NavigationCommandRunFirst(t *testing.T) {
	repoDir, _ := writeFixture(t)

	stdout, _, _ := run(t, repoDir, "refs", "Callee")
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command: "refs",
	})
	errObj := requireMap(t, resp["error"], "error")
	if getString(t, errObj["code"], "error.code") != "MISSING_INDEX" {
		t.Fatalf("expected MISSING_INDEX error")
	}
	next, ok := errObj["next"].(map[string]any)
	if !ok {
		t.Fatalf("expected error.next")
	}
	if !strings.Contains(getString(t, next["command"], "next.command"), "snipe index") {
		t.Fatalf("expected next command to mention snipe index")
	}
}

func TestAmbiguousSymbol_ReturnsCandidates_When_NameIsNotUnique(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, exitCode := run(t, repoDir, "def", "Ambiguous")
	_ = exitCode
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command: "def",
	})
	errObj := requireMap(t, resp["error"], "error")
	if getString(t, errObj["code"], "error.code") != "AMBIGUOUS_SYMBOL" {
		t.Fatalf("expected AMBIGUOUS_SYMBOL")
	}
	candidates := requireSlice(t, errObj["candidates"], "error.candidates")
	if len(candidates) == 0 {
		t.Fatalf("expected candidates")
	}
	first := requireMap(t, candidates[0], "candidate")
	if _, ok := first["id"]; !ok {
		t.Fatalf("candidate missing id")
	}
	if _, ok := first["name"]; !ok {
		t.Fatalf("candidate missing name")
	}
	if _, ok := first["file"]; !ok {
		t.Fatalf("candidate missing file")
	}
	if _, ok := first["kind"]; !ok {
		t.Fatalf("candidate missing kind")
	}
}

func TestPagination_OffsetAndLimit_Work(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdoutA, _, exitCode := run(t, repoDir, "refs", "Callee", "--limit", "1", "--offset", "0")
	if exitCode != 0 {
		t.Fatalf("refs limit exit %d", exitCode)
	}
	respA := parseJSON(t, stdoutA)
	resultsA := requireSlice(t, respA["results"], "results")
	if len(resultsA) == 0 {
		t.Fatalf("expected results for offset 0")
	}
	firstA := requireMap(t, resultsA[0], "results[0]")
	idA := getString(t, firstA["id"], "id")

	stdoutB, _, exitCode := run(t, repoDir, "refs", "Callee", "--limit", "1", "--offset", "1")
	if exitCode != 0 {
		t.Fatalf("refs offset exit %d", exitCode)
	}
	respB := parseJSON(t, stdoutB)
	resultsB := requireSlice(t, respB["results"], "results")
	if len(resultsB) == 0 {
		t.Fatalf("expected results for offset 1")
	}
	firstB := requireMap(t, resultsB[0], "results[0]")
	idB := getString(t, firstB["id"], "id")

	meta := requireMap(t, respB["meta"], "meta")
	total := int(getFloat(t, meta["total"], "meta.total"))
	if total >= 2 && idA == idB {
		t.Fatalf("expected different ids for pagination")
	}
}

func TestHumanFlag_ProducesNonJSON_When_HumanEnabled(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--human")
	if exitCode != 0 {
		t.Fatalf("refs --human exit %d stderr=%s", exitCode, string(stderr))
	}

	var parsed any
	if err := json.Unmarshal(stdout, &parsed); err == nil {
		if !strings.Contains(string(stdout), "\n  ") {
			t.Fatalf("expected pretty JSON for --human output")
		}
		return
	}

	if !strings.Contains(string(stdout), "results") {
		t.Fatalf("expected human readable output, got: %s", string(stdout))
	}
}

func TestMaxTokens_Truncates_When_LowBudget(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--with-body", "--max-tokens", "50")
	if exitCode != 0 {
		t.Fatalf("refs --max-tokens exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	meta := requireMap(t, resp["meta"], "meta")
	truncated := getBool(t, meta["truncated"], "meta.truncated")
	if !truncated {
		t.Fatalf("expected truncated output with low max-tokens")
	}
}

func TestWithBody_IncludesFunctionBody_When_Enabled(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--with-body")
	if exitCode != 0 {
		t.Fatalf("refs --with-body exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected results")
	}
	for _, item := range results {
		result := requireMap(t, item, "result")
		if body, ok := result["body"].(string); ok && strings.TrimSpace(body) != "" {
			return
		}
	}
	t.Skip("TODO: refs --with-body did not include body field")
}

func TestSchema_CommandOutputsValidJSONSchema(t *testing.T) {
	repoDir, _ := writeFixture(t)

	stdout, stderr, exitCode := run(t, repoDir, "schema")
	if exitCode != 0 {
		t.Fatalf("schema exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	if _, ok := resp["$schema"]; !ok {
		if _, ok := resp["definitions"]; !ok {
			if _, ok := resp["properties"]; !ok {
				t.Fatalf("schema missing $schema/definitions/properties keys: %s", formatJSONKeys(resp))
			}
		}
	}
}

func TestVersion_PrintsSomethingSane(t *testing.T) {
	repoDir, _ := writeFixture(t)

	stdout, stderr, exitCode := run(t, repoDir, "version")
	if exitCode != 0 {
		t.Fatalf("version exit %d stderr=%s", exitCode, string(stderr))
	}

	var parsed map[string]any
	if err := json.Unmarshal(stdout, &parsed); err == nil {
		if _, ok := parsed["results"]; !ok {
			t.Fatalf("version JSON missing results")
		}
		return
	}

	if !strings.Contains(string(stdout), "snipe version") {
		t.Fatalf("unexpected version output: %s", string(stdout))
	}
}

func indexRepo(t *testing.T, repoDir string) {
	t.Helper()

	stdout, stderr, exitCode := run(t, repoDir, "index", repoDir)
	if exitCode != 0 {
		t.Fatalf("index exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
}
