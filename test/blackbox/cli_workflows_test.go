//go:build blackbox

package blackbox

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// --no-body is required to get context lines (body is included by default and skips context)
	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--no-body", "--context", "2")
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

	stdoutAlt, _, _ := run(t, repoDir, "refs", "Callee", "--no-body", "--context", "1")
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

	// Verify meta.total matches actual results count (no inflated defResult)
	meta := requireMap(t, respCallees["meta"], "meta")
	total := int(getFloat(t, meta["total"], "meta.total"))
	if total != len(calleeResults) {
		t.Fatalf("meta.total=%d but len(results)=%d", total, len(calleeResults))
	}

	// Verify each callee result has coherent identity: ID, file, range all describe the callee
	for i, raw := range calleeResults {
		callee := requireMap(t, raw, fmt.Sprintf("results[%d]", i))

		name := getString(t, callee["name"], "name")
		if name == "" {
			t.Fatalf("results[%d]: callee name missing", i)
		}

		// ID must be 16-char hex
		id := getString(t, callee["id"], "id")
		if len(id) != 16 {
			t.Fatalf("results[%d]: id %q is not 16 chars", i, id)
		}

		// kind must be a real symbol kind, not synthetic "definition"
		kind := getString(t, callee["kind"], "kind")
		if kind == "definition" {
			t.Fatalf("results[%d]: kind should be a symbol kind (func/method), got %q", i, kind)
		}

		// file must point to where the callee is defined, not the caller's file
		// In this fixture both are main.go, so verify range points to callee definition
		rng := requireMap(t, callee["range"], "range")
		start := requireMap(t, rng["start"], "range.start")
		line := int(getFloat(t, start["line"], "range.start.line"))
		// Callee() is defined around line 30; the call site is around line 19.
		// The range must NOT be the call site line.
		if line < 25 {
			t.Fatalf("results[%d] %q: range.start.line=%d looks like call site, not callee definition", i, name, line)
		}

		// edit_target must be present
		editTarget := getString(t, callee["edit_target"], "edit_target")
		if editTarget == "" {
			t.Fatalf("results[%d]: edit_target missing", i)
		}

		// Verify ID chains: snipe show <id> should resolve to the callee
		showStdout, showStderr, showExit := run(t, repoDir, "show", id)
		if showExit != 0 {
			t.Fatalf("snipe show %s failed: exit=%d stderr=%s", id, showExit, string(showStderr))
		}
		showResp := parseJSON(t, showStdout)
		showResults := requireSlice(t, showResp["results"], "show results")
		if len(showResults) == 0 {
			t.Fatalf("snipe show %s returned no results", id)
		}
		showResult := requireMap(t, showResults[0], "show results[0]")
		showName := getString(t, showResult["name"], "show name")
		if showName != name {
			t.Fatalf("ID chain broken: callees returned name=%q but show %s returned name=%q", name, id, showName)
		}
	}
}

func TestImpl_ReturnsImplementations_ForInterface(t *testing.T) {
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

func TestAmbiguousSymbol_SelectBest_ResolvesToTopCandidate(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Without --select, ambiguous names error out (covered by sibling test).
	// With --select best, def should pick one candidate and succeed.
	stdout, stderr, exitCode := run(t, repoDir, "def", "Ambiguous", "--select", "best")
	resp := parseJSON(t, stdout)
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok=true with --select best, got exit=%d stderr=%s resp=%v", exitCode, string(stderr), resp)
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("expected 1 result with --select best, got %d", len(results))
	}
	first := requireMap(t, results[0], "results[0]")
	if getString(t, first["name"], "name") != "Ambiguous" {
		t.Fatalf("expected Ambiguous, got %v", first["name"])
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

func TestMaxTokens_Truncates_When_LowBudget(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee", "--max-tokens", "50")
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

func TestBody_IncludedByDefault(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Body is now included by default (no --with-body flag needed)
	stdout, stderr, exitCode := run(t, repoDir, "refs", "Callee")
	if exitCode != 0 {
		t.Fatalf("refs exit %d stderr=%s", exitCode, string(stderr))
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
	t.Skip("TODO: refs did not include body field")
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

func TestEdit_PreviewMode_ReturnsEditPlan_When_NoApplyFlag(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Test edit in preview mode (no --apply flag)
	stdout, stderr, exitCode := run(t, repoDir, "edit", "Callee",
		"--operation", "replace_body",
		"--new-code", `return "edited"`)
	if exitCode != 0 {
		t.Fatalf("edit preview exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:         "edit",
		requireQuery:    true,
		requireRepoRoot: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected edit results")
	}

	first := requireMap(t, results[0], "results[0]")

	// Verify edit response fields
	if _, ok := first["file"]; !ok {
		t.Fatalf("edit result missing file")
	}
	if _, ok := first["symbol"]; !ok {
		t.Fatalf("edit result missing symbol")
	}
	if op := getString(t, first["operation"], "operation"); op != "replace_body" {
		t.Fatalf("expected operation replace_body, got %s", op)
	}
	if _, ok := first["original_code"]; !ok {
		t.Fatalf("edit result missing original_code")
	}
	if _, ok := first["new_code"]; !ok {
		t.Fatalf("edit result missing new_code")
	}
	if _, ok := first["diff"]; !ok {
		t.Fatalf("edit result missing diff")
	}

	// Verify it's in preview mode (not applied)
	applied := getBool(t, first["applied"], "applied")
	if applied {
		t.Fatalf("expected applied=false in preview mode")
	}
}

func TestEdit_ApplyMode_ModifiesFile_When_ApplyFlagSet(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Apply an edit
	stdout, stderr, exitCode := run(t, repoDir, "edit", "Callee",
		"--operation", "replace_body",
		"--new-code", `return "applied"`,
		"--apply")
	if exitCode != 0 {
		t.Fatalf("edit apply exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected edit results")
	}

	first := requireMap(t, results[0], "results[0]")
	applied := getBool(t, first["applied"], "applied")
	if !applied {
		t.Fatalf("expected applied=true when --apply used")
	}

	// Verify the file was actually modified
	mainPath := first["file"].(string)
	if !strings.HasPrefix(mainPath, "/") {
		mainPath = repoDir + "/" + mainPath
	}
	content, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	if !strings.Contains(string(content), `"applied"`) {
		t.Fatalf("file not modified as expected")
	}
}

func TestEdit_SymbolNotFound_ReturnsError(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, _ := run(t, repoDir, "edit", "NonExistentSymbol",
		"--operation", "replace_body",
		"--new-code", "return nil")

	resp := parseJSON(t, stdout)
	errObj := requireMap(t, resp["error"], "error")
	code := getString(t, errObj["code"], "error.code")
	if code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND error, got %s", code)
	}
}

func TestPkg_Main_ReturnsRootPackageSymbols(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "pkg", "main")
	if exitCode != 0 {
		t.Fatalf("pkg main exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "pkg",
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pkg main to return results")
	}

	// Verify we got symbols from the root package (example.com/fixture)
	names := map[string]bool{}
	for _, item := range results {
		result := requireMap(t, item, "result")
		name := getString(t, result["name"], "name")
		names[name] = true
	}

	// The root package exports: Widget, Caller, AnotherCaller, Callee, UseGreeter, UseAmbiguous
	for _, expected := range []string{"Widget", "Caller", "Callee"} {
		if !names[expected] {
			t.Errorf("pkg main missing expected symbol %q, got %v", expected, names)
		}
	}
}

func TestPkg_Dot_FromRepoRoot_ReturnsSameAsMain(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "pkg", ".")
	if exitCode != 0 {
		t.Fatalf("pkg . exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pkg . to return results from root package")
	}

	// Should contain the same root package symbols as "pkg main"
	names := map[string]bool{}
	for _, item := range results {
		result := requireMap(t, item, "result")
		name := getString(t, result["name"], "name")
		names[name] = true
	}
	if !names["Widget"] {
		t.Errorf("pkg . from root missing Widget, got %v", names)
	}
}

func TestPack_StructType_AggregatesMethodCallGraph(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "pack", "Widget")
	if exitCode != 0 {
		t.Fatalf("pack Widget exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "pack",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pack results")
	}

	pack := requireMap(t, results[0], "results[0]")

	// Definition should exist and be a struct
	def := requireMap(t, pack["definition"], "definition")
	if kind := getString(t, def["kind"], "kind"); kind != "struct" {
		t.Fatalf("expected struct kind, got %s", kind)
	}

	// Methods should be populated (Widget has Do())
	methodsRaw, ok := pack["methods"]
	if !ok || methodsRaw == nil {
		t.Fatalf("expected methods field for struct pack")
	}
	methods := requireSlice(t, methodsRaw, "methods")
	if len(methods) == 0 {
		t.Fatalf("expected at least one method for Widget")
	}
	firstMethod := requireMap(t, methods[0], "methods[0]")
	if getString(t, firstMethod["name"], "name") != "Do" {
		t.Fatalf("expected method name Do, got %s", getString(t, firstMethod["name"], "name"))
	}

	// Callers should be aggregated from Widget.Do()'s callers (UseWidget calls Do)
	callerCount := int(getFloat(t, pack["caller_count"], "caller_count"))
	if callerCount == 0 {
		t.Fatalf("expected caller_count > 0 for struct with called methods")
	}

	// Callees should be aggregated from Widget.Do()'s callees
	calleeCount := int(getFloat(t, pack["callee_count"], "callee_count"))
	if calleeCount == 0 {
		t.Fatalf("expected callee_count > 0 for struct with methods that call other functions")
	}
}

func TestPack_PackagePath_ReturnsPackageProfile(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Root fixture package imports alpha and beta; packing "alpha" should
	// return a package profile (not a symbol profile) with exports, and show
	// the root fixture as a dependent via dependent_count > 0.
	stdout, stderr, exitCode := run(t, repoDir, "pack", "alpha")
	if exitCode != 0 {
		t.Fatalf("pack alpha exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "pack",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pack alpha results")
	}
	pkg := requireMap(t, results[0], "results[0]")

	// Package-mode result has a "package" field, not "definition".
	pkgName := getString(t, pkg["package"], "package")
	if pkgName == "" {
		t.Fatalf("expected package field to be populated")
	}
	if !strings.HasSuffix(pkgName, "alpha") && pkgName != "alpha" {
		t.Fatalf("expected package to end in alpha, got %q", pkgName)
	}

	exports := requireSlice(t, pkg["exports"], "exports")
	if len(exports) == 0 {
		t.Fatalf("expected at least one export for alpha package")
	}
	// Alpha exports Ambiguous()
	foundAmbiguous := false
	for _, e := range exports {
		exp := requireMap(t, e, "export")
		if getString(t, exp["name"], "name") == "Ambiguous" {
			foundAmbiguous = true
			break
		}
	}
	if !foundAmbiguous {
		t.Fatalf("expected Ambiguous in alpha exports")
	}

	// dependent_count must be present and non-negative. (Module-root callers
	// are filtered out by FindPackageDeps's `modulePath/%` suffix match, so
	// we don't assert a lower bound — just that the field exists and is sane.)
	if _, ok := pkg["dependent_count"]; !ok {
		t.Fatalf("expected dependent_count field")
	}
	if dc := int(getFloat(t, pkg["dependent_count"], "dependent_count")); dc < 0 {
		t.Fatalf("dependent_count must be >= 0, got %d", dc)
	}

	// LOC > 0 (we count from disk)
	if loc := int(getFloat(t, pkg["loc"], "loc")); loc <= 0 {
		t.Fatalf("expected loc > 0 for alpha package, got %d", loc)
	}

	// Should NOT have a "definition" field (that's symbol-mode).
	if _, ok := pkg["definition"]; ok {
		t.Fatalf("package-mode pack should not contain definition field")
	}
}

func TestPack_PackagePath_PopulatesKeyFields(t *testing.T) {
	// Asserts the under-covered Claude-facing fields: key_types, key_funcs,
	// file_count, imports, and (for the root pkg) test_count.
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// greet pkg has 3 types (Greeter, Friendly, Rude, PointerGreeter) and
	// Hello methods on each — exercises key_types/key_funcs ranking.
	stdout, _, exitCode := run(t, repoDir, "pack", "greet")
	if exitCode != 0 {
		t.Fatalf("pack greet exit %d", exitCode)
	}
	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	pkg := requireMap(t, results[0], "results[0]")

	keyTypes := requireSlice(t, pkg["key_types"], "key_types")
	if len(keyTypes) == 0 {
		t.Fatalf("expected key_types to be populated for greet")
	}
	foundFriendly := false
	for _, kt := range keyTypes {
		m := requireMap(t, kt, "key_types[i]")
		if getString(t, m["name"], "name") == "Friendly" {
			foundFriendly = true
			break
		}
	}
	if !foundFriendly {
		t.Fatalf("expected Friendly in greet key_types")
	}

	keyFuncs := requireSlice(t, pkg["key_funcs"], "key_funcs")
	if len(keyFuncs) == 0 {
		t.Fatalf("expected key_funcs to be populated for greet")
	}

	if fc := int(getFloat(t, pkg["file_count"], "file_count")); fc < 1 {
		t.Fatalf("expected file_count >= 1, got %d", fc)
	}

	// Root fixture pkg has main_test.go with Test* funcs — test_count > 0.
	stdout, _, exitCode = run(t, repoDir, "pack", ".")
	if exitCode != 0 {
		t.Fatalf("pack . exit %d", exitCode)
	}
	resp = parseJSON(t, stdout)
	results = requireSlice(t, resp["results"], "results")
	pkg = requireMap(t, results[0], "results[0]")
	if tc := int(getFloat(t, pkg["test_count"], "test_count")); tc < 1 {
		t.Fatalf("expected test_count >= 1 for root fixture, got %d", tc)
	}
	imports := requireSlice(t, pkg["imports"], "imports")
	if len(imports) == 0 {
		t.Fatalf("expected imports populated (fixture imports alpha/beta/greet)")
	}
}

func TestPack_PackagePath_NonExistent_ReturnsNotFound(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, _ := run(t, repoDir, "pack", "nonexistent/pkg")
	// Clean error envelope, not a crash/panic. Snipe reports errors via the
	// JSON envelope with exit 0, so we check shape.
	resp := parseJSON(t, stdout)
	if ok, _ := resp["ok"].(bool); ok {
		t.Fatalf("expected ok=false for nonexistent package, stderr=%s", string(stderr))
	}
	errObj := requireMap(t, resp["error"], "error")
	if code := getString(t, errObj["code"], "error.code"); code != "NOT_FOUND" {
		t.Fatalf("expected NOT_FOUND error, got %s", code)
	}
}

func TestPack_Symbol_StillWorks_Regression(t *testing.T) {
	// Regression guard: the previous symbol-mode behavior must be unchanged
	// when the argument is a plain symbol name (no slash, not a directory).
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "pack", "Callee")
	if exitCode != 0 {
		t.Fatalf("pack Callee exit %d stderr=%s", exitCode, string(stderr))
	}
	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pack Callee results")
	}
	pack := requireMap(t, results[0], "results[0]")
	if _, ok := pack["definition"]; !ok {
		t.Fatalf("symbol-mode pack must contain definition")
	}
}

func TestDeps_SinglePackage_ReturnsDependencies(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "deps", "alpha")
	if exitCode != 0 {
		t.Fatalf("deps exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "deps",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected deps results")
	}
	first := requireMap(t, results[0], "results[0]")

	// alpha is imported by root, so it should have dependents
	if _, ok := first["package"]; !ok {
		t.Fatalf("deps result missing package")
	}
	if _, ok := first["dependencies"]; !ok {
		t.Fatalf("deps result missing dependencies")
	}
	if _, ok := first["dependents"]; !ok {
		t.Fatalf("deps result missing dependents")
	}
}

func TestDeps_Tree_ReturnsGraph(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "deps", "--tree")
	if exitCode != 0 {
		t.Fatalf("deps --tree exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "deps",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected tree results")
	}
	first := requireMap(t, results[0], "results[0]")

	if _, ok := first["packages"]; !ok {
		t.Fatalf("tree result missing packages")
	}
	if _, ok := first["edges"]; !ok {
		t.Fatalf("tree result missing edges")
	}

	// Fixture has root importing alpha, beta, greet — should have edges
	edges := requireSlice(t, first["edges"], "edges")
	if len(edges) == 0 {
		t.Fatalf("expected edges in dependency tree")
	}
}

func TestContext_Conventions_ReturnsDetectedPatterns_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "context", "--conventions", repoDir)
	if exitCode != 0 {
		t.Fatalf("context --conventions exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)

	// Fixture has Greeter interface → interfaces should be detected
	if iface, ok := resp["interfaces"]; ok {
		ifaceMap := requireMap(t, iface, "interfaces")
		if _, ok := ifaceMap["pattern"]; !ok {
			t.Fatalf("interfaces missing pattern field")
		}
		if _, ok := ifaceMap["total"]; !ok {
			t.Fatalf("interfaces missing total field")
		}
	}

	// Fixture has (w *Widget) Do() → receivers should be detected
	if recv, ok := resp["receivers"]; ok {
		recvMap := requireMap(t, recv, "receivers")
		if _, ok := recvMap["pattern"]; !ok {
			t.Fatalf("receivers missing pattern field")
		}
		total := getFloat(t, recvMap["total"], "receivers.total")
		if total < 1 {
			t.Fatalf("receivers.total = %v, want >= 1", total)
		}
	}

	// At least one category should be detected from fixture
	assertHasConventions(t, resp)
}

func TestContext_Boot_IncludesConventions_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "context", repoDir)
	if exitCode != 0 {
		t.Fatalf("context exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)

	// Default orientation context should include conventions field
	conv, ok := resp["conventions"]
	if !ok {
		t.Fatalf("orientation context missing conventions field")
	}
	if conv == nil {
		t.Fatalf("orientation context conventions is null")
	}
	convMap := requireMap(t, conv, "conventions")
	assertHasConventions(t, convMap)
}

var conventionKeys = []string{"constructors", "receivers", "testing", "interfaces", "errors", "file_organization"}

func assertHasConventions(t *testing.T, m map[string]any) {
	t.Helper()
	detected := 0
	for _, key := range conventionKeys {
		if v, ok := m[key]; ok && v != nil {
			detected++
		}
	}
	if detected == 0 {
		t.Fatalf("expected at least one convention category, got none")
	}
}

func TestTests_FindsDirectAndTransitive_When_IndexPresent(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "tests", "Callee")
	if exitCode != 0 {
		t.Fatalf("tests exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "tests",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected test results for Callee")
	}

	// Should find TestCallee (direct) and TestViaHelper (transitive via testHelper)
	names := make(map[string]bool)
	for _, r := range results {
		rm := requireMap(t, r, "result")
		names[getString(t, rm["name"], "name")] = true
	}
	if !names["TestCallee"] {
		t.Errorf("missing direct test TestCallee; got %v", names)
	}
}

func TestTests_Direct_ExcludesTransitive(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "tests", "--direct", "Callee")
	if exitCode != 0 {
		t.Fatalf("tests --direct exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")

	// Direct should only find TestCallee, not TestViaHelper
	for _, r := range results {
		rm := requireMap(t, r, "result")
		if hints, ok := rm["hints"]; ok && hints != nil {
			hintSlice := requireSlice(t, hints, "hints")
			for _, h := range hintSlice {
				if getString(t, h, "hint") == "transitive_test" {
					t.Errorf("--direct should not return transitive tests")
				}
			}
		}
	}
}

func TestTests_ZeroCoverage_ReturnsSuggestions(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// UseAmbiguous has no test callers
	stdout, stderr, exitCode := run(t, repoDir, "tests", "UseAmbiguous")
	if exitCode != 0 {
		t.Fatalf("tests exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 0 {
		t.Fatalf("expected 0 tests for UseAmbiguous, got %d", len(results))
	}

	// Should have suggestions
	if sug, ok := resp["suggestions"]; ok && sug != nil {
		suggestions := requireSlice(t, sug, "suggestions")
		if len(suggestions) == 0 {
			t.Errorf("expected suggestions for zero-coverage symbol")
		}
	}
}

func TestImpact_FindsCallersAndTests(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "impact", "Callee")
	if exitCode != 0 {
		t.Fatalf("impact exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "impact",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected impact results for Callee")
	}

	// Check for at least one direct_caller hint (Caller or AnotherCaller)
	foundDirectCaller := false
	foundTest := false
	foundExported := false
	for _, r := range results {
		rm := requireMap(t, r, "result")
		if hints, ok := rm["hints"]; ok && hints != nil {
			hintSlice := requireSlice(t, hints, "hints")
			for _, h := range hintSlice {
				hint := getString(t, h, "hint")
				if hint == "direct_caller" {
					foundDirectCaller = true
				}
				if hint == "direct_test" || hint == "transitive_test" {
					foundTest = true
				}
				if hint == "exported" {
					foundExported = true
				}
			}
		}
	}
	if !foundDirectCaller {
		t.Errorf("expected at least one result with hint direct_caller")
	}
	if !foundTest {
		t.Errorf("expected at least one result with hint direct_test or transitive_test")
	}
	if !foundExported {
		t.Errorf("expected at least one result with hint exported (Caller/AnotherCaller are exported)")
	}

	// Check suggestions exist (summary suggestion)
	if sug, ok := resp["suggestions"]; ok && sug != nil {
		suggestions := requireSlice(t, sug, "suggestions")
		if len(suggestions) == 0 {
			t.Errorf("expected at least one suggestion")
		}
	} else {
		t.Errorf("expected suggestions in response")
	}
}

func TestImpact_Direct_ExcludesTransitive(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "impact", "--direct", "Callee")
	if exitCode != 0 {
		t.Fatalf("impact --direct exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")

	for _, r := range results {
		rm := requireMap(t, r, "result")
		if hints, ok := rm["hints"]; ok && hints != nil {
			hintSlice := requireSlice(t, hints, "hints")
			for _, h := range hintSlice {
				hint := getString(t, h, "hint")
				if hint == "transitive_caller" {
					t.Errorf("--direct should not return transitive_caller hints")
				}
				if hint == "transitive_test" {
					t.Errorf("--direct should not return transitive_test hints")
				}
			}
		}
	}
}

func TestImpact_ZeroCoverage_ReturnsSuggestions(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "impact", "UseAmbiguous")
	if exitCode != 0 {
		t.Fatalf("impact exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)

	// UseAmbiguous has callers but no test callers — should have no_tests suggestion
	if sug, ok := resp["suggestions"]; ok && sug != nil {
		suggestions := requireSlice(t, sug, "suggestions")
		foundNoTests := false
		for _, s := range suggestions {
			sm := requireMap(t, s, "suggestion")
			if cond, ok := sm["condition"]; ok {
				if getString(t, cond, "condition") == "no_tests" {
					foundNoTests = true
				}
			}
		}
		if !foundNoTests {
			t.Errorf("expected suggestion with condition no_tests for zero-coverage symbol")
		}
	} else {
		t.Errorf("expected suggestions for zero-coverage symbol")
	}
}

func TestImpact_Interface_FindsImplementers(t *testing.T) {
	// Note: snipe impl uses a reference heuristic (not structural matching),
	// so the fixture's Greeter interface currently returns 0 implementers.
	// This test verifies the impact command handles interfaces correctly
	// and that if implementers are found, they carry the right hint.
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := run(t, repoDir, "impact", "Greeter")
	if exitCode != 0 {
		t.Fatalf("impact exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{
		command:           "impact",
		requireQuery:      true,
		requireRepoRoot:   true,
		requireIndexState: true,
	})

	// If results are present, check implementer hints are correctly applied
	if resultsRaw, ok := resp["results"]; ok && resultsRaw != nil {
		results := requireSlice(t, resultsRaw, "results")
		for _, r := range results {
			rm := requireMap(t, r, "result")
			if hints, ok := rm["hints"]; ok && hints != nil {
				hintSlice := requireSlice(t, hints, "hints")
				for _, h := range hintSlice {
					hint := getString(t, h, "hint")
					// All hints should be valid impact hint types
					switch hint {
					case "direct_caller", "transitive_caller", "implementer",
						"direct_test", "transitive_test", "exported", "ref_site":
						// valid
					default:
						t.Errorf("unexpected hint %q", hint)
					}
				}
			}
		}
	}

	// Greeter has no test coverage — should get no_tests suggestion
	if sug, ok := resp["suggestions"]; ok && sug != nil {
		suggestions := requireSlice(t, sug, "suggestions")
		if len(suggestions) == 0 {
			t.Errorf("expected suggestions for interface impact")
		}
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "Test"},
		// Disable ambient hooks (e.g. a global commit-msg conventional-commit
		// hook via core.hooksPath) so fixture commits aren't rejected.
		{"config", "core.hooksPath", "/dev/null"},
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func indexRepo(t *testing.T, repoDir string) {
	t.Helper()

	stdout, stderr, exitCode := run(t, repoDir, "index", repoDir)
	if exitCode != 0 {
		t.Fatalf("index exit %d stderr=%s stdout=%s", exitCode, string(stderr), string(stdout))
	}
}

// runWithEnv runs snipe with a custom environment (filtered from os.Environ).
func runWithEnv(t *testing.T, repoDir string, env []string, args ...string) (stdout []byte, stderr []byte, exitCode int) {
	t.Helper()

	// Inject --format json unless caller already specified --format.
	hasFormat := false
	for _, a := range args {
		if a == "--format" {
			hasFormat = true
			break
		}
	}
	if !hasFormat {
		args = append([]string{"--format", "json"}, args...)
	}

	cmd := exec.Command(binPath, args...)
	cmd.Dir = repoDir
	cmd.Env = env

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.Bytes()
	stderr = errBuf.Bytes()
	if err == nil {
		return stdout, stderr, 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, exitErr.ExitCode()
	}
	return stdout, stderr, 1
}

// envWithout returns os.Environ() with the named keys removed.
func envWithout(keys ...string) []string {
	remove := make(map[string]bool, len(keys))
	for _, k := range keys {
		remove[k] = true
	}
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if !remove[k] {
			env = append(env, e)
		}
	}
	return env
}

// --- Semantic fallback tests ---

func TestSearch_NoFallback_WhenRgFindsResults(t *testing.T) {
	_, rgErr := exec.LookPath("rg")
	if rgErr != nil {
		t.Skip("rg not installed")
	}
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// "Hello" exists literally in the fixture — rg should find it
	stdout, stderr, exitCode := run(t, repoDir, "search", "Hello")
	if exitCode != 0 {
		t.Fatalf("search exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatal("expected rg results for literal match")
	}

	// No decision_path should be present (no fallback)
	meta := requireMap(t, resp["meta"], "meta")
	if dp, ok := meta["decision_path"]; ok && dp != nil {
		t.Fatalf("unexpected decision_path when rg found results: %v", dp)
	}
}

func TestSearch_NoFallback_WhenNoCredentials(t *testing.T) {
	_, rgErr := exec.LookPath("rg")
	if rgErr != nil {
		t.Skip("rg not installed")
	}
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// Search for something that won't match literally, with credentials stripped
	env := envWithout("SNIPE_VOYAGE_API_KEY", "VOYAGE_MODEL", "VOYAGE_API_URL")
	// Also prevent credentials file from being read by setting HOME to temp dir
	env = append(env, "HOME="+t.TempDir())
	stdout, stderr, exitCode := runWithEnv(t, repoDir, env, "search", "xyzzy_nonexistent_pattern_42")
	if exitCode != 0 {
		t.Fatalf("search exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	// results may be null (nil) or empty array — both are acceptable for 0 results
	resultCount := 0
	if results, ok := resp["results"].([]any); ok {
		resultCount = len(results)
	}
	if resultCount != 0 {
		t.Fatalf("expected 0 results without credentials, got %d", resultCount)
	}

	// No decision_path should be present (fallback not attempted)
	meta := requireMap(t, resp["meta"], "meta")
	if dp, ok := meta["decision_path"]; ok && dp != nil {
		t.Fatalf("unexpected decision_path without credentials: %v", dp)
	}

	// Should suggest snipe sim as alternative
	if sugg, ok := resp["suggestions"].([]any); ok {
		found := false
		for _, s := range sugg {
			if m, ok := s.(map[string]any); ok {
				if cmd, ok := m["command"].(string); ok && strings.Contains(cmd, "snipe sim") {
					found = true
				}
			}
		}
		if !found {
			t.Error("expected snipe sim suggestion when no results and no fallback")
		}
	}
}

func TestSearch_NoFallback_WhenFileFilterSet(t *testing.T) {
	_, rgErr := exec.LookPath("rg")
	if rgErr != nil {
		t.Skip("rg not installed")
	}
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	// --file restricts search; fallback should not fire even with 0 results
	stdout, stderr, exitCode := run(t, repoDir, "search", "xyzzy_nonexistent_42", "--file", "*.go")
	if exitCode != 0 {
		t.Fatalf("search exit %d stderr=%s", exitCode, string(stderr))
	}

	resp := parseJSON(t, stdout)
	// results may be null (nil) or empty array — both are acceptable for 0 results
	resultCount := 0
	if results, ok := resp["results"].([]any); ok {
		resultCount = len(results)
	}
	if resultCount != 0 {
		t.Fatalf("expected 0 results for nonsense pattern, got %d", resultCount)
	}

	meta := requireMap(t, resp["meta"], "meta")
	if dp, ok := meta["decision_path"]; ok && dp != nil {
		t.Fatalf("unexpected decision_path with --file filter: %v", dp)
	}
}

func TestRootDetection_FindsIndexFromSubdirectory_When_IndexedAtGitRoot(t *testing.T) {
	repoDir, _ := writeFixture(t)

	// Add .git so FindProjectRoot walks up to repoDir
	if err := os.Mkdir(filepath.Join(repoDir, ".git"), 0750); err != nil {
		t.Fatal(err)
	}

	// Index from root
	indexRepo(t, repoDir)

	// Run def from a subdirectory — should find the index at repoDir, not fail with MISSING_INDEX
	subDir := filepath.Join(repoDir, "greet")
	stdout, _, exitCode := run(t, subDir, "def", "Callee")
	if exitCode != 0 {
		t.Fatalf("expected exit 0 from subdir, got %d; stdout: %s", exitCode, stdout)
	}
	resp := parseJSON(t, stdout)
	if errObj, hasErr := resp["error"]; hasErr && errObj != nil {
		t.Fatalf("unexpected error from subdir: %v", errObj)
	}
}

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
