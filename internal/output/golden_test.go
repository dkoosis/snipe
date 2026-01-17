package output

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// Golden file paths relative to testdata
const (
	goldenDir           = "../../testdata/golden"
	goldenDefOutput     = "def_output.json"
	goldenRefsOutput    = "refs_output.json"
	goldenSearchOutput  = "search_output.json"
	goldenCallersOutput = "callers_output.json"
)

// TestGoldenDefOutput tests def command output against golden file
func TestGoldenDefOutput(t *testing.T) {
	resp := Response[Result]{
		Results: []Result{
			{
				ID:   "go#main.go#main",
				File: "main.go",
				Range: Range{
					Start: Position{Line: 5, Col: 1},
					End:   Position{Line: 10, Col: 2},
				},
				Kind:       "func",
				Name:       "main",
				Match:      "func main() {",
				EditTarget: "main.go:5:1-10:2",
			},
		},
		Meta: Meta{
			Command:    "def",
			IndexState: IndexFresh,
			Ms:         15,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenDefOutput, resp)
}

// TestGoldenRefsOutput tests refs command output against golden file
func TestGoldenRefsOutput(t *testing.T) {
	resp := Response[Result]{
		Results: []Result{
			{
				ID:   "ref:main.go:15:10",
				File: "main.go",
				Range: Range{
					Start: Position{Line: 15, Col: 10},
					End:   Position{Line: 15, Col: 14},
				},
				Kind:  "ref",
				Name:  "main",
				Match: "    main()",
				Enclosing: &Enclosing{
					ID:   "go#main.go#init",
					Kind: "func",
					Name: "init",
				},
				EditTarget: "main.go:15:10-15:14",
			},
			{
				ID:   "ref:cmd/root.go:42:5",
				File: "cmd/root.go",
				Range: Range{
					Start: Position{Line: 42, Col: 5},
					End:   Position{Line: 42, Col: 9},
				},
				Kind:       "ref",
				Name:       "main",
				Match:      "main.Run()",
				EditTarget: "cmd/root.go:42:5-42:9",
			},
		},
		Meta: Meta{
			Command:    "refs",
			IndexState: IndexFresh,
			Ms:         23,
			Total:      2,
			Limit:      50,
		},
	}

	testGolden(t, goldenRefsOutput, resp)
}

// TestGoldenSearchOutput tests search command output against golden file
func TestGoldenSearchOutput(t *testing.T) {
	resp := Response[Result]{
		Results: []Result{
			{
				ID:   "s1510",
				File: "internal/search/rg.go",
				Range: Range{
					Start: Position{Line: 15, Col: 10},
					End:   Position{Line: 15, Col: 16},
				},
				Kind:       "match",
				Name:       "Search",
				Match:      "func Search(dir, pattern string, limit, contextLines int) ([]output.Result, error) {",
				EditTarget: "internal/search/rg.go:15:10-15:16",
			},
		},
		Meta: Meta{
			Command:    "search",
			IndexState: IndexFresh,
			Ms:         8,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenSearchOutput, resp)
}

// TestGoldenCallersOutput tests callers command output against golden file
func TestGoldenCallersOutput(t *testing.T) {
	resp := Response[Result]{
		Results: []Result{
			{
				ID:   "caller:cmd/search.go:25",
				File: "cmd/search.go",
				Range: Range{
					Start: Position{Line: 25, Col: 12},
					End:   Position{Line: 25, Col: 18},
				},
				Kind:  "caller",
				Name:  "runSearch",
				Match: "    results, err := search.Search(dir, pattern, limit, ctx)",
				Enclosing: &Enclosing{
					ID:        "go#cmd/search.go#runSearch",
					Kind:      "func",
					Name:      "runSearch",
					Signature: "func runSearch(cmd *cobra.Command, args []string) error",
				},
				EditTarget: "cmd/search.go:25:12-25:18",
			},
		},
		Meta: Meta{
			Command:    "callers",
			IndexState: IndexFresh,
			Ms:         12,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenCallersOutput, resp)
}

// testGolden compares a response against a golden file
func testGolden(t *testing.T, filename string, resp interface{}) {
	t.Helper()

	goldenPath := filepath.Join(goldenDir, filename)

	got, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}
	got = append(got, '\n')

	if *update {
		if err := os.WriteFile(goldenPath, got, 0644); err != nil {
			t.Fatalf("Failed to update golden file: %v", err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("Golden file not found: %s\nRun with -update to create it", goldenPath)
		}
		t.Fatalf("Failed to read golden file: %v", err)
	}

	if string(got) != string(want) {
		t.Errorf("Output does not match golden file %s\n\nGot:\n%s\n\nWant:\n%s\n\nRun with -update flag to regenerate golden files",
			filename, string(got), string(want))
	}
}

// TestGoldenSchemaConsistency verifies that the output schema is consistent
func TestGoldenSchemaConsistency(t *testing.T) {
	// Verify Response[Result] JSON schema has required fields
	resp := Response[Result]{
		Results: []Result{
			{
				ID:         "test",
				File:       "test.go",
				Range:      Range{Start: Position{Line: 1, Col: 1}, End: Position{Line: 1, Col: 5}},
				Kind:       "func",
				Name:       "test",
				EditTarget: "test.go:1:1-1:5",
			},
		},
		Meta: Meta{
			Command:    "test",
			IndexState: IndexFresh,
			Total:      1,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify required top-level fields exist
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	requiredFields := []string{"results", "meta"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Verify meta structure
	meta, ok := raw["meta"].(map[string]interface{})
	if !ok {
		t.Fatal("meta is not an object")
	}
	metaFields := []string{"command", "index_state", "total"}
	for _, field := range metaFields {
		if _, ok := meta[field]; !ok {
			t.Errorf("Missing required meta field: %s", field)
		}
	}

	// Verify result structure
	results, ok := raw["results"].([]interface{})
	if !ok || len(results) == 0 {
		t.Fatal("results is not a non-empty array")
	}
	result, ok := results[0].(map[string]interface{})
	if !ok {
		t.Fatal("result is not an object")
	}
	resultFields := []string{"id", "file", "range", "kind", "name", "edit_target"}
	for _, field := range resultFields {
		if _, ok := result[field]; !ok {
			t.Errorf("Missing required result field: %s", field)
		}
	}
}
