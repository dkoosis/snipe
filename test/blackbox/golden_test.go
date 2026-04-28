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

func assertGoldenText(t *testing.T, repoRoot string, raw []byte) {
	t.Helper()

	got := normalizeText(string(raw), repoRoot)
	goldenPath := filepath.Join("testdata", "golden", sanitizeTestName(t.Name())+".txt")
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if string(expected) != got {
		t.Fatalf("golden mismatch\n--- expected\n%s\n--- got\n%s", string(expected), got)
	}
}

func normalizeText(s, repoRoot string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if repoRoot != "" {
		s = strings.ReplaceAll(s, repoRoot, "<repo>")
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n"
}

// normalizeDiagram strips the D2 source block from diagram output, keeping
// only the human-readable summary and call tree. The D2 source contains
// non-deterministic hex node IDs, but the tree above it is stable.
func normalizeDiagram(s, repoRoot string) string {
	if i := strings.Index(s, "```d2"); i != -1 {
		s = strings.TrimRight(s[:i], " \t\n")
		s += "\n"
	}
	return normalizeText(s, repoRoot)
}

func assertGoldenDiagram(t *testing.T, repoRoot string, raw []byte) {
	t.Helper()

	got := normalizeDiagram(string(raw), repoRoot)
	goldenPath := filepath.Join("testdata", "golden", sanitizeTestName(t.Name())+".txt")
	if os.Getenv("UPDATE_GOLDENS") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if string(expected) != got {
		t.Fatalf("golden mismatch\n--- expected\n%s\n--- got\n%s", string(expected), got)
	}
}
