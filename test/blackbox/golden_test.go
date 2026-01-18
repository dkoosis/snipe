//go:build blackbox

package blackbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertGoldenJSON(t *testing.T, repoRoot string, raw []byte) {
	t.Helper()

	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("invalid JSON for golden compare: %v\n%s", err, string(raw))
	}

	normalized := normalize(parsed, repoRoot)
	pretty := prettyJSON(t, normalized)

	goldenPath := filepath.Join("testdata", "golden", sanitizeTestName(t.Name())+".json")
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if err := os.WriteFile(goldenPath, pretty, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	if string(expected) != string(pretty) {
		t.Fatalf("golden mismatch\n--- expected\n%s\n--- got\n%s", string(expected), string(pretty))
	}
}

func sanitizeTestName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}
