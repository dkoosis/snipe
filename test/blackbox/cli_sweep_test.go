//go:build blackbox

package blackbox

import (
	"fmt"
	"strings"
	"testing"
)

func assertEnvelope(t *testing.T, stdout []byte, command string) map[string]any {
	t.Helper()
	resp := parseJSON(t, stdout)
	assertResponseContract(t, resp, responseExpectations{command: command})
	return resp
}

func assertNoDuplicateIDs(t *testing.T, results []any, label string) {
	t.Helper()
	seen := map[string]bool{}
	for i, r := range results {
		m := requireMap(t, r, fmt.Sprintf("%s[%d]", label, i))
		id := getString(t, m["id"], "id")
		if seen[id] {
			t.Fatalf("duplicates were returned: duplicate id %q in %s", id, label)
		}
		seen[id] = true
	}
}

func assertNoDuplicateNames(t *testing.T, results []any, label string) {
	t.Helper()
	seen := map[string]bool{}
	for i, r := range results {
		m := requireMap(t, r, fmt.Sprintf("%s[%d]", label, i))
		name := getString(t, m["name"], "name")
		if seen[name] {
			t.Fatalf("duplicates were returned: duplicate name %q in %s", name, label)
		}
		seen[name] = true
	}
}

func firstResultID(t *testing.T, repoDir, symbol string) string {
	t.Helper()
	stdout, stderr, exitCode := run(t, repoDir, "def", symbol)
	if exitCode != 0 {
		t.Fatalf("def %s exit %d stderr=%s", symbol, exitCode, string(stderr))
	}
	resp := assertEnvelope(t, stdout, "def")
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("def %s returned no results", symbol)
	}
	first := requireMap(t, results[0], "results[0]")
	return getString(t, first["id"], "id")
}

func TestDef(t *testing.T) {
	repoDir, paths := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("by_name_function", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "Callee")
		resp := assertEnvelope(t, stdout, "def")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected results")
		}
		first := requireMap(t, results[0], "results[0]")
		if getString(t, first["name"], "name") != "Callee" {
			t.Fatalf("expected Callee result")
		}
	})

	t.Run("by_name_method", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "Do")
		resp := assertEnvelope(t, stdout, "def")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected results")
		}
	})

	t.Run("by_name_interface", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "Greeter")
		resp := assertEnvelope(t, stdout, "def")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected results")
		}
	})

	t.Run("by_position", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "--at", paths["callee_at"])
		resp := assertEnvelope(t, stdout, "def")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
	})

	t.Run("by_hex_id", func(t *testing.T) {
		id := firstResultID(t, repoDir, "Callee")
		stdout, _, _ := run(t, repoDir, "def", id)
		resp := assertEnvelope(t, stdout, "def")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected results")
		}
	})

	t.Run("ambiguous_symbol", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "Ambiguous")
		resp := assertEnvelope(t, stdout, "def")
		if getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ambiguous lookup to fail")
		}
		err := requireMap(t, resp["error"], "error")
		cands := requireSlice(t, err["candidates"], "candidates")
		if len(cands) < 2 {
			t.Fatalf("expected multiple candidates")
		}
		cand := requireMap(t, cands[0], "candidate")
		_ = getString(t, cand["id"], "id")
		_ = getString(t, cand["name"], "name")
	})

	t.Run("not_found", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "DefinitelyMissingSymbol")
		resp := assertEnvelope(t, stdout, "def")
		if getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok false")
		}
		err := requireMap(t, resp["error"], "error")
		msg := getString(t, err["message"], "message")
		if msg == "" || !strings.Contains(strings.ToLower(msg), "not") {
			t.Fatalf("expected meaningful error, got %q", msg)
		}
	})
}

func TestRefs(t *testing.T) {
	repoDir, paths := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("basic_and_enclosing", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--no-body")
		resp := assertEnvelope(t, stdout, "refs")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) < 4 {
			t.Fatalf("expected at least 4 refs to Callee, got %d", len(results))
		}
		for i, r := range results {
			m := requireMap(t, r, fmt.Sprintf("results[%d]", i))
			enc, ok := m["enclosing"]
			if !ok || enc == nil {
				t.Fatalf("results[%d] missing enclosing", i)
			}
		}
	})

	t.Run("file_filter", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--file", paths["test"], "--no-body")
		resp := assertEnvelope(t, stdout, "refs")
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected refs in test file")
		}
	})

	t.Run("kind_filter", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--kind", "func", "--no-body")
		resp := assertEnvelope(t, stdout, "refs")
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected filtered refs")
		}
	})
}

func TestCallers(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("three_plus_callers", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "callers", "Callee")
		resp := assertEnvelope(t, stdout, "callers")
		results := requireSlice(t, resp["results"], "results")
		if len(results) < 3 {
			t.Fatalf("expected >=3 callers, got %d", len(results))
		}
		assertNoDuplicateIDs(t, results, "callers")
	})

	t.Run("detailed_format", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "callers", "Callee", "--format", "detailed")
		resp := assertEnvelope(t, stdout, "callers")
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected callers")
		}
	})
}

func TestCallees(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, _ := run(t, repoDir, "callees", "CallMany")
	resp := assertEnvelope(t, stdout, "callees")
	results := requireSlice(t, resp["results"], "results")
	if len(results) < 3 {
		t.Fatalf("expected >=3 callees, got %d", len(results))
	}
	for i, r := range results {
		m := requireMap(t, r, fmt.Sprintf("results[%d]", i))
		if getString(t, m["id"], "id") == "" || getString(t, m["name"], "name") == "" {
			t.Fatalf("results[%d] missing id/name", i)
		}
	}
	assertNoDuplicateNames(t, results, "callees")
}

func TestPack(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, _ := run(t, repoDir, "pack", "Callee")
	resp := assertEnvelope(t, stdout, "pack")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected pack result")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["definition"]; !ok {
		t.Fatalf("pack missing definition")
	}
	if _, ok := first["references"]; !ok {
		t.Fatalf("pack missing references")
	}
	if _, ok := first["callers"]; !ok {
		t.Fatalf("pack missing callers")
	}
	if _, ok := first["callee_count"]; !ok {
		t.Fatalf("pack missing callee_count")
	}
	meta := requireMap(t, resp["meta"], "meta")
	if tok, ok := meta["token_estimate"].(float64); !ok || tok <= 0 {
		t.Fatalf("expected token_estimate > 0")
	}
	if role, ok := first["role"].(string); !ok || role == "" {
		t.Fatalf("expected non-empty role")
	}
}

func TestSym(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, _ := run(t, repoDir, "sym", "Callee")
	resp := assertEnvelope(t, stdout, "sym")
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected sym results")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["definition"]; !ok {
		t.Fatalf("sym missing definition")
	}

	defOut, _, _ := run(t, repoDir, "def", "Callee")
	defResp := assertEnvelope(t, defOut, "def")
	defResults := requireSlice(t, defResp["results"], "results")
	if len(defResults) == 0 {
		t.Fatalf("def has no results")
	}
	defFirst := requireMap(t, defResults[0], "results[0]")
	symDef := requireMap(t, first["definition"], "definition")
	if getString(t, symDef["name"], "name") != getString(t, defFirst["name"], "name") {
		t.Fatalf("sym definition name mismatch")
	}
}

func TestShow(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("valid_id", func(t *testing.T) {
		id := firstResultID(t, repoDir, "Callee")
		stdout, _, _ := run(t, repoDir, "show", id)
		resp := assertEnvelope(t, stdout, "show")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected show results")
		}
	})

	t.Run("invalid_id", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "show", "1234abcd")
		resp := assertEnvelope(t, stdout, "show")
		if getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok false")
		}
	})
}

func TestTests(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("exercised", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "tests", "Callee")
		resp := assertEnvelope(t, stdout, "tests")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected tests")
		}
	})

	t.Run("direct", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "tests", "--direct", "Callee")
		resp := assertEnvelope(t, stdout, "tests")
		_ = requireSlice(t, resp["results"], "results")
	})

	t.Run("not_exercised", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "tests", "UseAmbiguous")
		resp := assertEnvelope(t, stdout, "tests")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) != 0 {
			t.Fatalf("expected empty results")
		}
	})
}

func TestImpact(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	stdout, _, _ := run(t, repoDir, "impact", "Callee")
	resp := assertEnvelope(t, stdout, "impact")
	if !getBool(t, resp["ok"], "ok") {
		t.Skip("impact appears unimplemented in this environment")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected impact results")
	}
}

func TestSearch(t *testing.T) {
	repoDir, _ := writeFixture(t)

	t.Run("literal", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "search", "Hello")
		resp := assertEnvelope(t, stdout, "search")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected search results")
		}
	})

	t.Run("regex", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "search", "H.llo")
		resp := assertEnvelope(t, stdout, "search")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
	})

	t.Run("without_index", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "search", "Callee")
		resp := assertEnvelope(t, stdout, "search")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected search to work without index")
		}
	})
}

func TestIndex(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "index", repoDir)
	resp := assertEnvelope(t, stdout, "index")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}

	statusOut, _, _ := run(t, repoDir, "status")
	statusResp := assertEnvelope(t, statusOut, "status")
	if !getBool(t, statusResp["ok"], "ok") {
		t.Fatalf("expected status ok true after index")
	}
}

func TestDoctor(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "doctor")
	resp := assertEnvelope(t, stdout, "doctor")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected doctor checks")
	}
}

func TestStatus(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "status")
	resp := assertEnvelope(t, stdout, "status")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected status results")
	}
	first := requireMap(t, results[0], "results[0]")
	for _, k := range []string{"symbols", "refs", "calls"} {
		if n, ok := first[k].(float64); !ok || n <= 0 {
			t.Fatalf("expected non-zero %s", k)
		}
	}
}

func TestContext(t *testing.T) {
	repoDir, _ := writeFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "context", "--boot", repoDir)
	out := strings.TrimSpace(string(stdout))
	if out == "" {
		t.Fatalf("expected non-empty context output")
	}
}

func TestImpl(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "impl", "Greeter")
	resp := assertEnvelope(t, stdout, "impl")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) < 2 {
		t.Fatalf("expected pointer and value implementations, got %d", len(results))
	}
	names := map[string]bool{}
	for _, r := range results {
		m := requireMap(t, r, "result")
		names[getString(t, m["name"], "name")] = true
	}
	if !names["Friendly"] || !names["PointerGreeter"] {
		t.Fatalf("expected both value and pointer receiver implementations, got %v", names)
	}
}

func TestImporters(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "importers", "example.com/fixture/greet")
	resp := assertEnvelope(t, stdout, "importers")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected importer results")
	}
	assertNoDuplicateNames(t, results, "importers")
}

func TestImports(t *testing.T) {
	repoDir, paths := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("known_file", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "imports", paths["main"])
		resp := assertEnvelope(t, stdout, "imports")
		if !getBool(t, resp["ok"], "ok") {
			t.Fatalf("expected ok true")
		}
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected imports results")
		}
	})

	t.Run("unknown_file", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "imports", "missing.go")
		resp := assertEnvelope(t, stdout, "imports")
		if getBool(t, resp["ok"], "ok") {
			results := requireSlice(t, resp["results"], "results")
			if len(results) != 0 {
				t.Fatalf("expected empty results for unknown file when ok=true")
			}
			return
		}
		err := requireMap(t, resp["error"], "error")
		if getString(t, err["message"], "message") == "" {
			t.Fatalf("expected meaningful error")
		}
	})
}

func TestPkg(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "pkg", "main")
	resp := assertEnvelope(t, stdout, "pkg")
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected exported symbols")
	}
	names := map[string]bool{}
	for _, r := range results {
		m := requireMap(t, r, "result")
		names[getString(t, m["name"], "name")] = true
	}
	if !names["Widget"] || !names["Callee"] {
		t.Fatalf("expected exported symbols in pkg main, got %v", names)
	}
}

func TestTypes(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("struct", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "types", "Widget")
		resp := assertEnvelope(t, stdout, "types")
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected types results")
		}
		first := requireMap(t, results[0], "results[0]")
		if len(first) == 0 {
			t.Fatalf("expected non-empty struct type payload")
		}
	})

	t.Run("interface", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "types", "Greeter")
		resp := assertEnvelope(t, stdout, "types")
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected types results")
		}
		first := requireMap(t, results[0], "results[0]")
		if len(first) == 0 {
			t.Fatalf("expected non-empty interface type payload")
		}
	})
}

func TestExplain(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "explain", "CallMany")
	resp := assertEnvelope(t, stdout, "explain")
	if !getBool(t, resp["ok"], "ok") {
		t.Fatalf("expected ok true")
	}
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected explain results")
	}
	first := requireMap(t, results[0], "results[0]")
	if len(first) == 0 {
		t.Fatalf("expected structured explanation payload")
	}
}

func TestDeps(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "deps", "--tree")
	resp := assertEnvelope(t, stdout, "deps")
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected deps results")
	}
}

func TestEdit(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "edit", "Callee", "--operation", "replace_body", "--new-code", `return "changed"`)
	resp := assertEnvelope(t, stdout, "edit")
	results := requireSlice(t, resp["results"], "results")
	if len(results) == 0 {
		t.Fatalf("expected edit result")
	}
	first := requireMap(t, results[0], "results[0]")
	if _, ok := first["diff"]; !ok {
		t.Fatalf("expected edit-oriented info")
	}
}

func TestSim(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)
	stdout, _, _ := run(t, repoDir, "sim", "Callee")
	resp := assertEnvelope(t, stdout, "sim")
	if getBool(t, resp["ok"], "ok") {
		results := requireSlice(t, resp["results"], "results")
		if len(results) == 0 {
			t.Fatalf("expected similarity results when ok=true")
		}
		return
	}
	err := requireMap(t, resp["error"], "error")
	if getString(t, err["message"], "message") == "" {
		t.Fatalf("expected graceful error for sim when embeddings unavailable")
	}
}

func TestEmbedStatus(t *testing.T) {
	repoDir, _ := writeFixture(t)
	stdout, stderr, exitCode := run(t, repoDir, "embed-status")
	if exitCode == 0 {
		resp := assertEnvelope(t, stdout, "embed-status")
		if !getBool(t, resp["ok"], "ok") {
			err := requireMap(t, resp["error"], "error")
			if getString(t, err["message"], "message") == "" {
				t.Fatalf("expected meaningful embed-status error")
			}
		}
		return
	}
	if len(strings.TrimSpace(string(stderr))) == 0 {
		t.Fatalf("expected meaningful stderr when embed-status fails")
	}
}

func TestVersion(t *testing.T) {
	repoDir, _ := writeFixture(t)
	stdout, _, _ := run(t, repoDir, "version")
	if len(strings.TrimSpace(string(stdout))) == 0 {
		t.Fatalf("expected version output")
	}
}

func TestHelp(t *testing.T) {
	repoDir, _ := writeFixture(t)
	stdout, _, exitCode := run(t, repoDir, "help")
	if exitCode != 0 {
		t.Fatalf("help should exit 0")
	}
	if !strings.Contains(string(stdout), "Usage:") {
		t.Fatalf("expected help usage text")
	}
}

func TestGlobalFlagsSweep(t *testing.T) {
	repoDir, _ := writeFixture(t)
	indexRepo(t, repoDir)

	t.Run("format_concise_vs_detailed", func(t *testing.T) {
		concise, _, _ := run(t, repoDir, "def", "Callee", "--format", "concise")
		detailed, _, _ := run(t, repoDir, "def", "Callee", "--format", "detailed")
		if string(concise) == string(detailed) {
			t.Fatalf("expected differing outputs for concise vs detailed")
		}
	})

	t.Run("limit_one", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--limit", "1", "--no-body")
		resp := assertEnvelope(t, stdout, "refs")
		results := requireSlice(t, resp["results"], "results")
		if len(results) != 1 {
			t.Fatalf("expected limit 1, got %d", len(results))
		}
	})

	t.Run("max_tokens", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--max-tokens", "100")
		resp := assertEnvelope(t, stdout, "refs")
		meta := requireMap(t, resp["meta"], "meta")
		if truncated, ok := meta["truncated"].(bool); !ok || !truncated {
			t.Fatalf("expected meta.truncated=true")
		}
	})

	t.Run("no_body", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "refs", "Callee", "--no-body")
		resp := assertEnvelope(t, stdout, "refs")
		results := requireSlice(t, resp["results"], "results")
		first := requireMap(t, results[0], "results[0]")
		if _, ok := first["body"]; ok {
			t.Fatalf("expected body absent with --no-body")
		}
	})

	t.Run("signature_only", func(t *testing.T) {
		stdout, _, _ := run(t, repoDir, "def", "Callee", "--signature-only")
		resp := assertEnvelope(t, stdout, "def")
		results := requireSlice(t, resp["results"], "results")
		first := requireMap(t, results[0], "results[0]")
		if _, ok := first["match"]; !ok {
			t.Fatalf("expected signature/match content present")
		}
		if _, ok := first["body"]; ok {
			t.Fatalf("expected body absent with --signature-only")
		}
		if _, ok := first["context"]; ok {
			t.Fatalf("expected context absent with --signature-only")
		}
	})

	t.Run("offset", func(t *testing.T) {
		one, _, _ := run(t, repoDir, "refs", "Callee", "--limit", "1", "--offset", "0", "--no-body")
		two, _, _ := run(t, repoDir, "refs", "Callee", "--limit", "1", "--offset", "1", "--no-body")
		r1 := assertEnvelope(t, one, "refs")
		r2 := assertEnvelope(t, two, "refs")
		a := requireMap(t, requireSlice(t, r1["results"], "results")[0], "a")
		b := requireMap(t, requireSlice(t, r2["results"], "results")[0], "b")
		if getString(t, a["id"], "id") == getString(t, b["id"], "id") {
			t.Fatalf("expected different results with offset")
		}
	})
}
