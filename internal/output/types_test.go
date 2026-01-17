package output

import (
	"encoding/json"
	"testing"
)

func TestResponseMarshal(t *testing.T) {
	resp := Response[Result]{
		Results: []Result{
			{
				ID:   "abc123",
				File: "main.go",
				Range: Range{
					Start: Position{Line: 10, Col: 5},
					End:   Position{Line: 10, Col: 15},
				},
				Kind:       "func",
				Name:       "main",
				Match:      "func main()",
				EditTarget: "main.go:10:5-10:15",
			},
		},
		Meta: Meta{
			Command:    "def",
			IndexState: IndexFresh,
			Ms:         42,
			Total:      1,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify it can be unmarshaled back
	var got Response[Result]
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if got.Meta.Command != "def" {
		t.Errorf("Command = %q, want %q", got.Meta.Command, "def")
	}
	if len(got.Results) != 1 {
		t.Fatalf("Results count = %d, want 1", len(got.Results))
	}
	if got.Results[0].Name != "main" {
		t.Errorf("Result name = %q, want %q", got.Results[0].Name, "main")
	}
}

func TestErrorMarshal(t *testing.T) {
	resp := Response[any]{
		Results: nil,
		Meta: Meta{
			Command:    "def",
			IndexState: IndexMissing,
		},
		Error: NewMissingIndexError(),
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var got Response[any]
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if got.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if got.Error.Code != ErrMissingIndex {
		t.Errorf("Error code = %q, want %q", got.Error.Code, ErrMissingIndex)
	}
	if got.Error.Next == nil {
		t.Fatal("Next action should not be nil")
	}
	if got.Error.Next.Command != "snipe index" {
		t.Errorf("Next command = %q, want %q", got.Error.Next.Command, "snipe index")
	}
}

func TestFormatEditTarget(t *testing.T) {
	r := Range{
		Start: Position{Line: 42, Col: 10},
		End:   Position{Line: 42, Col: 25},
	}

	// Without hash
	got := FormatEditTarget("main.go", r, "")
	want := "main.go:42:10-42:25"
	if got != want {
		t.Errorf("FormatEditTarget() without hash = %q, want %q", got, want)
	}

	// With hash
	got = FormatEditTarget("main.go", r, "abc123def456")
	want = "main.go:42:10-42:25@abc123def456"
	if got != want {
		t.Errorf("FormatEditTarget() with hash = %q, want %q", got, want)
	}
}

func TestComputeRangeHash(t *testing.T) {
	// Test with invalid file returns empty string
	hash := ComputeRangeHash("/nonexistent/file.go", Range{
		Start: Position{Line: 1, Col: 1},
		End:   Position{Line: 1, Col: 10},
	})
	if hash != "" {
		t.Errorf("ComputeRangeHash() for nonexistent file = %q, want empty string", hash)
	}

	// Test with invalid range returns empty string
	// (can't test without actual file, but we test the structure)
}

func TestFormatEditTargetWithHash(t *testing.T) {
	// For nonexistent file, should return target without hash
	r := Range{
		Start: Position{Line: 1, Col: 1},
		End:   Position{Line: 1, Col: 10},
	}
	got := FormatEditTargetWithHash("/nonexistent/file.go", r)
	want := "/nonexistent/file.go:1:1-1:10"
	if got != want {
		t.Errorf("FormatEditTargetWithHash() for nonexistent file = %q, want %q", got, want)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"func main() {}", 4},
	}

	for _, tt := range tests {
		got := EstimateTokens(tt.input)
		if got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNewAmbiguousError(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Name: "Config", File: "a/config.go", Kind: "type"},
		{ID: "b", Name: "Config", File: "b/config.go", Kind: "type"},
	}
	err := NewAmbiguousError("Config", candidates)

	if err.Code != ErrAmbiguousSymbol {
		t.Errorf("Code = %q, want %q", err.Code, ErrAmbiguousSymbol)
	}
	if len(err.Candidates) != 2 {
		t.Errorf("Candidates count = %d, want 2", len(err.Candidates))
	}
}

// TestAmbiguousSymbolJSONFormat verifies the JSON output matches SPEC.md format
func TestAmbiguousSymbolJSONFormat(t *testing.T) {
	candidates := []Candidate{
		{ID: "abc", Name: "Config", File: "config/config.go", Kind: "type"},
		{ID: "def", Name: "Config", File: "server/config.go", Kind: "type"},
	}
	err := NewAmbiguousError("Config", candidates)

	// Marshal to JSON
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("Marshal failed: %v", marshalErr)
	}

	// Verify structure by parsing
	var parsed map[string]interface{}
	if unmarshalErr := json.Unmarshal(data, &parsed); unmarshalErr != nil {
		t.Fatalf("Unmarshal failed: %v", unmarshalErr)
	}

	// Verify error code matches SPEC
	code, ok := parsed["code"].(string)
	if !ok || code != "AMBIGUOUS_SYMBOL" {
		t.Errorf("code = %v, want %q", parsed["code"], "AMBIGUOUS_SYMBOL")
	}

	// Verify message exists
	message, ok := parsed["message"].(string)
	if !ok || message == "" {
		t.Error("message should be a non-empty string")
	}

	// Verify candidates array exists with correct structure
	candidatesRaw, ok := parsed["candidates"].([]interface{})
	if !ok || len(candidatesRaw) != 2 {
		t.Fatalf("candidates should be an array of 2, got %v", parsed["candidates"])
	}

	// Verify each candidate has required fields per SPEC
	for i, cRaw := range candidatesRaw {
		c, ok := cRaw.(map[string]interface{})
		if !ok {
			t.Errorf("candidate[%d] should be an object", i)
			continue
		}

		// SPEC requires: id, name, file, kind
		requiredFields := []string{"id", "name", "file", "kind"}
		for _, field := range requiredFields {
			if _, exists := c[field]; !exists {
				t.Errorf("candidate[%d] missing required field %q", i, field)
			}
		}
	}
}

// TestAmbiguousSymbolDeterminism verifies response is deterministic per SPEC
func TestAmbiguousSymbolDeterminism(t *testing.T) {
	candidates := []Candidate{
		{ID: "abc", Name: "Config", File: "config/config.go", Kind: "type"},
		{ID: "def", Name: "Config", File: "server/config.go", Kind: "type"},
	}

	// Create same error multiple times
	err1 := NewAmbiguousError("Config", candidates)
	err2 := NewAmbiguousError("Config", candidates)

	// Marshal both
	data1, _ := json.Marshal(err1)
	data2, _ := json.Marshal(err2)

	// Verify identical output
	if string(data1) != string(data2) {
		t.Error("NewAmbiguousError should produce deterministic output")
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	// Create test results
	results := []Result{
		{Name: "foo", File: "a.go", Match: "func foo()"},
		{Name: "bar", File: "b.go", Match: "func bar()"},
		{Name: "baz", File: "c.go", Match: "func baz()"},
	}

	t.Run("zero budget means no truncation", func(t *testing.T) {
		got, truncated := TruncateToTokenBudget(results, 0)
		if truncated {
			t.Error("should not be truncated with 0 budget")
		}
		if len(got) != len(results) {
			t.Errorf("got %d results, want %d", len(got), len(results))
		}
	})

	t.Run("large budget keeps all results", func(t *testing.T) {
		got, truncated := TruncateToTokenBudget(results, 10000)
		if truncated {
			t.Error("should not be truncated with large budget")
		}
		if len(got) != len(results) {
			t.Errorf("got %d results, want %d", len(got), len(results))
		}
	})

	t.Run("small budget truncates results", func(t *testing.T) {
		// Very small budget should still return at least one result
		got, truncated := TruncateToTokenBudget(results, 300)
		if !truncated {
			t.Error("should be truncated with small budget")
		}
		if len(got) == 0 {
			t.Error("should return at least one result")
		}
		if len(got) >= len(results) {
			t.Error("should have fewer results than input")
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		got, truncated := TruncateToTokenBudget(nil, 1000)
		if truncated {
			t.Error("should not be truncated with empty input")
		}
		if len(got) != 0 {
			t.Error("should return empty slice")
		}
	})
}

func TestEstimateResultTokens(t *testing.T) {
	result := Result{
		Name:  "ProcessOrder",
		File:  "order/handler.go",
		Match: "func ProcessOrder(ctx context.Context, order *Order) error",
	}

	tokens := EstimateResultTokens(&result)
	if tokens < 50 {
		t.Errorf("tokens = %d, expected at least 50 (includes overhead)", tokens)
	}

	// Adding body should increase token count
	result.Body = "func ProcessOrder(ctx context.Context, order *Order) error {\n\treturn nil\n}"
	tokensWithBody := EstimateResultTokens(&result)
	if tokensWithBody <= tokens {
		t.Errorf("tokens with body (%d) should be greater than without (%d)", tokensWithBody, tokens)
	}
}

func TestScoreResult(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		query    string
		minScore float64
		maxScore float64
	}{
		{
			name:     "exact match exported function",
			result:   Result{Name: "ProcessOrder", Kind: "func", File: "a.go"},
			query:    "ProcessOrder",
			minScore: 140, // 100 (exact) + 30 (func) + 20 (exported) - path penalty
			maxScore: 160,
		},
		{
			name:     "exact match case insensitive",
			result:   Result{Name: "ProcessOrder", Kind: "func", File: "a.go"},
			query:    "processorder",
			minScore: 140,
			maxScore: 160,
		},
		{
			name:     "prefix match",
			result:   Result{Name: "ProcessOrder", Kind: "func", File: "a.go"},
			query:    "Process",
			minScore: 90, // 50 (prefix) + 30 (func) + 20 (exported)
			maxScore: 110,
		},
		{
			name:     "contains match",
			result:   Result{Name: "ProcessOrder", Kind: "func", File: "a.go"},
			query:    "Order",
			minScore: 65, // 25 (contains) + 30 (func) + 20 (exported)
			maxScore: 85,
		},
		{
			name:     "unexported lower score",
			result:   Result{Name: "processOrder", Kind: "func", File: "a.go"},
			query:    "processOrder",
			minScore: 120, // 100 (exact) + 30 (func) + 0 (unexported)
			maxScore: 140,
		},
		{
			name:     "reference kind lower score",
			result:   Result{Name: "ProcessOrder", Kind: "ref", File: "a.go"},
			query:    "ProcessOrder",
			minScore: 110, // 100 (exact) + 0 (ref) + 20 (exported)
			maxScore: 130,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ScoreResult(&tt.result, tt.query)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("ScoreResult() = %.1f, want between %.1f and %.1f", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestSortByScore(t *testing.T) {
	results := []Result{
		{Name: "c", Score: 10},
		{Name: "a", Score: 100},
		{Name: "b", Score: 50},
	}

	SortByScore(results)

	if results[0].Name != "a" || results[1].Name != "b" || results[2].Name != "c" {
		t.Errorf("SortByScore() order = [%s, %s, %s], want [a, b, c]",
			results[0].Name, results[1].Name, results[2].Name)
	}
}

func TestScoreAndSort(t *testing.T) {
	results := []Result{
		{Name: "handler", Kind: "func", File: "a.go"},       // contains match
		{Name: "HandleRequest", Kind: "func", File: "b.go"}, // prefix match
		{Name: "handle", Kind: "func", File: "c.go"},        // exact match
	}

	ScoreAndSort(results, "handle")

	// Exact match should be first
	if results[0].Name != "handle" {
		t.Errorf("first result should be exact match 'handle', got %q", results[0].Name)
	}

	// Prefix match should be second
	if results[1].Name != "HandleRequest" {
		t.Errorf("second result should be prefix match 'HandleRequest', got %q", results[1].Name)
	}

	// All should have non-zero scores
	for i, r := range results {
		if r.Score == 0 {
			t.Errorf("result[%d] should have non-zero score", i)
		}
	}
}
