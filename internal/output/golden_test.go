package output

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/protocol"
)

var update = flag.Bool("update", false, "update golden files")

// Golden file paths relative to testdata
const (
	goldenDir              = "../../testdata/golden"
	goldenDefOutput        = "def_output.json"
	goldenRefsOutput       = "refs_output.json"
	goldenSearchOutput     = "search_output.json"
	goldenCallersOutput    = "callers_output.json"
	goldenNotFoundOutput   = "not_found_output.json"
	goldenAmbiguousOutput  = "ambiguous_output.json"
	goldenMissingIndexOut  = "missing_index_output.json"
	goldenStaleIndexOutput = "stale_index_output.json"
)

// TestGoldenDefOutput tests def command output against golden file
func TestGoldenDefOutput(t *testing.T) {
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:   "go#main.go#main",
				File: "main.go",
				Range: protocol.Range{
					Start: protocol.Position{Line: 5, Col: 1},
					End:   protocol.Position{Line: 10, Col: 2},
				},
				Kind:       "func",
				Name:       "main",
				Match:      "func main() {",
				EditTarget: "main.go:5:1-10:2",
			},
		},
		Meta: protocol.Meta{
			Command:    "def",
			IndexState: protocol.IndexFresh,
			Ms:         15,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenDefOutput, resp)
}

// TestGoldenRefsOutput tests refs command output against golden file
func TestGoldenRefsOutput(t *testing.T) {
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:   "ref:main.go:15:10",
				File: "main.go",
				Range: protocol.Range{
					Start: protocol.Position{Line: 15, Col: 10},
					End:   protocol.Position{Line: 15, Col: 14},
				},
				Kind:  "ref",
				Name:  "main",
				Match: "    main()",
				Enclosing: &protocol.Enclosing{
					ID:   "go#main.go#init",
					Kind: "func",
					Name: "init",
				},
				EditTarget: "main.go:15:10-15:14",
			},
			{
				ID:   "ref:cmd/root.go:42:5",
				File: "cmd/root.go",
				Range: protocol.Range{
					Start: protocol.Position{Line: 42, Col: 5},
					End:   protocol.Position{Line: 42, Col: 9},
				},
				Kind:       "ref",
				Name:       "main",
				Match:      "main.Run()",
				EditTarget: "cmd/root.go:42:5-42:9",
			},
		},
		Meta: protocol.Meta{
			Command:    "refs",
			IndexState: protocol.IndexFresh,
			Ms:         23,
			Total:      2,
			Limit:      50,
		},
	}

	testGolden(t, goldenRefsOutput, resp)
}

// TestGoldenSearchOutput tests search command output against golden file
func TestGoldenSearchOutput(t *testing.T) {
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:   "s1510",
				File: "internal/search/rg.go",
				Range: protocol.Range{
					Start: protocol.Position{Line: 15, Col: 10},
					End:   protocol.Position{Line: 15, Col: 16},
				},
				Kind:       "match",
				Name:       "Search",
				Match:      "func Search(dir, pattern string, limit, contextLines int) ([]output.Result, error) {",
				EditTarget: "internal/search/rg.go:15:10-15:16",
			},
		},
		Meta: protocol.Meta{
			Command:    "search",
			IndexState: protocol.IndexFresh,
			Ms:         8,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenSearchOutput, resp)
}

// TestGoldenCallersOutput tests callers command output against golden file
func TestGoldenCallersOutput(t *testing.T) {
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:   "caller:cmd/search.go:25",
				File: "cmd/search.go",
				Range: protocol.Range{
					Start: protocol.Position{Line: 25, Col: 12},
					End:   protocol.Position{Line: 25, Col: 18},
				},
				Kind:  "caller",
				Name:  "runSearch",
				Match: "    results, err := search.Search(dir, pattern, limit, ctx)",
				Enclosing: &protocol.Enclosing{
					ID:        "go#cmd/search.go#runSearch",
					Kind:      "func",
					Name:      "runSearch",
					Signature: "func runSearch(cmd *cobra.Command, args []string) error",
				},
				EditTarget: "cmd/search.go:25:12-25:18",
			},
		},
		Meta: protocol.Meta{
			Command:    "callers",
			IndexState: protocol.IndexFresh,
			Ms:         12,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenCallersOutput, resp)
}

// TestGoldenNotFoundOutput tests NOT_FOUND error envelope
func TestGoldenNotFoundOutput(t *testing.T) {
	resp := protocol.Response[any]{
		Protocol: protocol.ProtocolVersion,
		Ok:       false,
		Results:  nil,
		Meta: protocol.Meta{
			Command:    "def",
			IndexState: protocol.IndexFresh,
			Ms:         5,
		},
		Error: protocol.NewNotFoundError("Foo", "FooBar", "FooHandler"),
		Suggestions: []protocol.Suggestion{
			{Command: "snipe search Foo", Description: "Try text search", Priority: 2},
		},
	}

	testGolden(t, goldenNotFoundOutput, resp)
}

// TestGoldenAmbiguousOutput tests AMBIGUOUS_SYMBOL error envelope
func TestGoldenAmbiguousOutput(t *testing.T) {
	candidates := []protocol.Candidate{
		{ID: "abc123def45601", Name: "Config", File: "config/config.go", Kind: "type"},
		{ID: "abc123def45602", Name: "Config", File: "server/config.go", Kind: "type"},
	}
	// Mirror the production WriteError path: suggestions flow from the error
	// itself (err.Suggestions), not a separate re-call of the helper. This makes
	// the golden assert the real shipped output -- if NewAmbiguousError stopped
	// populating Suggestions, the hints would vanish and this test would fail.
	err := protocol.NewAmbiguousError("Config", candidates)
	resp := protocol.Response[any]{
		Protocol: protocol.ProtocolVersion,
		Ok:       false,
		Results:  nil,
		Meta: protocol.Meta{
			Command:    "def",
			IndexState: protocol.IndexFresh,
			Ms:         8,
		},
		Error:       err,
		Suggestions: err.Suggestions,
	}

	testGolden(t, goldenAmbiguousOutput, resp)
}

// TestGoldenMissingIndexOutput tests MISSING_INDEX error envelope with NextAction
func TestGoldenMissingIndexOutput(t *testing.T) {
	resp := protocol.Response[any]{
		Protocol: protocol.ProtocolVersion,
		Ok:       false,
		Results:  nil,
		Meta: protocol.Meta{
			Command:    "def",
			IndexState: protocol.IndexMissing,
			Ms:         1,
		},
		Error: protocol.NewMissingIndexError(),
	}

	testGolden(t, goldenMissingIndexOut, resp)
}

// TestGoldenStaleIndexOutput tests STALE_INDEX warning (ok:true with degraded)
func TestGoldenStaleIndexOutput(t *testing.T) {
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:         "go#main.go#main",
				File:       "main.go",
				Range:      protocol.Range{Start: protocol.Position{Line: 5, Col: 1}, End: protocol.Position{Line: 10, Col: 2}},
				Kind:       "func",
				Name:       "main",
				Match:      "func main() {",
				EditTarget: "main.go:5:1-10:2",
			},
		},
		Meta: protocol.Meta{
			Command:    "def",
			IndexState: protocol.IndexStale,
			Degraded:   []string{"stale_index"},
			Ms:         15,
			Total:      1,
			Limit:      50,
		},
	}

	testGolden(t, goldenStaleIndexOutput, resp)
}

// callerResults returns nCode callers from code files, then nTest from
// _test.go files, interleaved so the writer's split is what orders them.
func callerResults(nCode, nTest int) []protocol.Result {
	var rs []protocol.Result
	for i := range max(nCode, nTest) {
		if i < nTest {
			rs = append(rs, protocol.Result{
				ID: fmt.Sprintf("%016x", 100+i), Name: fmt.Sprintf("TestCase%d", i), Kind: "func",
				File: "store/store_test.go", Range: protocol.Range{Start: protocol.Position{Line: 10 + i}},
				Match: fmt.Sprintf("func TestCase%d(t *testing.T)", i),
			})
		}
		if i < nCode {
			rs = append(rs, protocol.Result{
				ID: fmt.Sprintf("%016x", i), Name: fmt.Sprintf("caller%d", i), Kind: "func",
				File: "cmd/root.go", Range: protocol.Range{Start: protocol.Position{Line: 10 + i}},
				Match: fmt.Sprintf("func caller%d() error", i),
			})
		}
	}
	return rs
}

// TestGoldenText_ListCap pins the default text of a list (sn-lvub.13): the
// cap line, code results before _test.go results, the empty state, and no
// `! noembed` on verbs that never read embeddings.
func TestGoldenText_ListCap(t *testing.T) {
	cases := []struct {
		name          string
		command       string
		nCode, nTest  int
		limitExplicit bool
		truncated     bool
		total         int // 0: len(results)
	}{
		{"callers_capped_tests_folded", "callers", 12, 5, false, false, 0},
		{"callers_tests_split_capped", "callers", 3, 9, false, false, 0},
		{"callers_tests_split", "callers", 2, 2, false, false, 0},
		{"callers_limit_shows_all", "callers", 9, 2, true, false, 0},
		{"refs_empty", "refs", 0, 0, false, false, 0},
		{"def_uncapped", "def", 10, 0, false, false, 0},
		{"callers_fetch_truncated", "callers", 12, 3, false, true, 0},
		{"refs_total_past_page", "refs", 10, 0, false, false, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.limitExplicit {
				SetListCap(0)
				t.Cleanup(func() { SetListCap(DefaultListCap) })
			}
			results := callerResults(tc.nCode, tc.nTest)
			resp := protocol.Response[protocol.Result]{
				Protocol: protocol.ProtocolVersion,
				Ok:       true,
				Results:  results,
				Meta: protocol.Meta{
					Command:    tc.command,
					Query:      map[string]string{"symbol": "Open"},
					IndexState: protocol.IndexFresh,
					Total:      len(results),
					Limit:      50,
					Truncated:  tc.truncated,
				},
			}
			if tc.truncated {
				resp.Meta.Limit = len(results)
			}
			if tc.total > 0 {
				resp.Meta.Total = tc.total
			}
			var buf bytes.Buffer
			w := NewWriter(&buf, OutputClaude)
			w.SetEmbedMissing(true)
			if err := w.WriteResponse(resp); err != nil {
				t.Fatalf("WriteResponse: %v", err)
			}
			testGoldenText(t, "text_"+tc.name+".txt", buf.String())
		})
	}
}

// testGoldenText compares rendered text against a golden file.
func testGoldenText(t *testing.T, filename, got string) {
	t.Helper()
	goldenPath := filepath.Join(goldenDir, filename)
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("Failed to update golden file: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("text does not match %s\n\nGot:\n%s\nWant:\n%s", filename, got, want)
	}
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
	resp := protocol.Response[protocol.Result]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results: []protocol.Result{
			{
				ID:         "test",
				File:       "test.go",
				Range:      protocol.Range{Start: protocol.Position{Line: 1, Col: 1}, End: protocol.Position{Line: 1, Col: 5}},
				Kind:       "func",
				Name:       "test",
				EditTarget: "test.go:1:1-1:5",
			},
		},
		Meta: protocol.Meta{
			Command:    "test",
			IndexState: protocol.IndexFresh,
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

	requiredFields := []string{"protocol", "ok", "results", "meta"}
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
