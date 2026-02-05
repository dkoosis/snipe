//go:build blackbox

package blackbox

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type responseExpectations struct {
	command           string
	requireQuery      bool
	requireRepoRoot   bool
	requireDegraded   bool
	requireIndexState bool
}

func parseJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, string(data))
	}
	return resp
}

func assertResponseContract(t *testing.T, resp map[string]any, expect responseExpectations) {
	t.Helper()

	allowedKeys := map[string]bool{"protocol": true, "ok": true, "results": true, "meta": true, "error": true, "suggestions": true}
	for key := range resp {
		if !allowedKeys[key] {
			t.Fatalf("unexpected top-level key %q", key)
		}
	}
	if _, ok := resp["results"]; !ok {
		t.Fatalf("missing top-level results")
	}
	metaVal, ok := resp["meta"]
	if !ok {
		t.Fatalf("missing top-level meta")
	}
	if _, ok := resp["error"]; !ok {
		t.Fatalf("missing top-level error")
	}

	meta := requireMap(t, metaVal, "meta")
	hasError := resp["error"] != nil
	assertMetaFields(t, meta, expect, !hasError)
	assertErrorField(t, resp["error"])
}

func assertMetaFields(t *testing.T, meta map[string]any, expect responseExpectations, requireTotals bool) {
	t.Helper()

	command, ok := meta["command"].(string)
	if !ok || command == "" {
		t.Fatalf("meta.command missing or not string")
	}
	if expect.command != "" && command != expect.command {
		t.Fatalf("meta.command = %q, want %q", command, expect.command)
	}

	if expect.requireQuery {
		if _, ok := meta["query"]; !ok {
			t.Fatalf("meta.query missing")
		}
	}
	if expect.requireRepoRoot {
		if _, ok := meta["repo_root"]; !ok {
			t.Fatalf("meta.repo_root missing")
		}
	}
	if expect.requireIndexState {
		if _, ok := meta["index_state"]; !ok {
			t.Fatalf("meta.index_state missing")
		}
	}
	if expect.requireDegraded {
		if _, ok := meta["degraded"]; !ok {
			t.Fatalf("meta.degraded missing")
		}
	}

	if _, ok := meta["ms"]; !ok {
		t.Fatalf("meta.ms missing")
	}
	if requireTotals {
		if _, ok := meta["total"]; !ok {
			t.Fatalf("meta.total missing")
		}
		if _, ok := meta["truncated"]; !ok {
			t.Fatalf("meta.truncated missing")
		}
	}
}

func assertErrorField(t *testing.T, value any) {
	t.Helper()

	if value == nil {
		return
	}

	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("error should be object or null")
	}
	code, ok := m["code"].(string)
	if !ok || code == "" {
		t.Fatalf("error.code missing or not string")
	}
	message, ok := m["message"].(string)
	if !ok || message == "" {
		t.Fatalf("error.message missing or not string")
	}
}

func requireMap(t *testing.T, value any, field string) map[string]any {
	t.Helper()

	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s should be object", field)
	}
	return m
}

func requireSlice(t *testing.T, value any, field string) []any {
	t.Helper()

	s, ok := value.([]any)
	if !ok {
		t.Fatalf("%s should be array", field)
	}
	return s
}

func normalize(value any, repoRoot string) any {
	switch v := value.(type) {
	case map[string]any:
		normalized := make(map[string]any)
		for key, val := range v {
			if isUnstableKey(key) {
				continue
			}
			if key == "repo_root" {
				normalized[key] = "<repo>"
				continue
			}
			normalized[key] = normalize(val, repoRoot)
		}
		return normalized
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalize(item, repoRoot))
		}
		return out
	case string:
		if repoRoot != "" && strings.Contains(v, repoRoot) {
			return strings.ReplaceAll(v, repoRoot, "<repo>")
		}
		return v
	default:
		return v
	}
}

func isUnstableKey(key string) bool {
	switch key {
	case "ms", "token_estimate", "indexed_at", "generated_at", "fingerprint", "git_commit", "id", "index_state":
		return true
	default:
		return false
	}
}

func prettyJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("pretty JSON: %v", err)
	}
	return append(data, '\n')
}

func getString(t *testing.T, value any, field string) string {
	t.Helper()

	s, ok := value.(string)
	if !ok {
		t.Fatalf("%s should be string", field)
	}
	return s
}

func getBool(t *testing.T, value any, field string) bool {
	t.Helper()

	b, ok := value.(bool)
	if !ok {
		t.Fatalf("%s should be bool", field)
	}
	return b
}

func getFloat(t *testing.T, value any, field string) float64 {
	t.Helper()

	n, ok := value.(float64)
	if !ok {
		t.Fatalf("%s should be number", field)
	}
	return n
}

func formatJSONKeys(value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return fmt.Sprintf("%T", value)
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}
