package output

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponseMarshal(t *testing.T) {
	resp := Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
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
		Protocol: ProtocolVersion,
		Ok:       false,
		Results:  nil,
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
	if got.Error.Next.Description == "" {
		t.Error("Next description should not be empty")
	}
}

func TestNewIndexInProgressError(t *testing.T) {
	err := NewIndexInProgressError()
	if err.Code != ErrIndexInProgress {
		t.Errorf("Code = %q, want %q", err.Code, ErrIndexInProgress)
	}
	if err.Next == nil {
		t.Fatal("Next should not be nil for INDEX_IN_PROGRESS")
	}
	if err.Next.Description == "" {
		t.Error("Next description should not be empty")
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
	// Use relative path for output, absolute for hash computation
	got := FormatEditTargetWithHash("file.go", "/nonexistent/file.go", r)
	want := "file.go:1:1-1:10"
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

func TestTruncateBodySemantic(t *testing.T) {
	t.Run("no truncation needed", func(t *testing.T) {
		result := Result{Body: "line1\nline2\nline3"}
		truncated := TruncateBodySemantic(&result, 5)
		if truncated {
			t.Error("should not truncate when lines fit")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		result := Result{Body: ""}
		truncated := TruncateBodySemantic(&result, 5)
		if truncated {
			t.Error("should not truncate empty body")
		}
	})

	t.Run("truncate at statement boundary", func(t *testing.T) {
		body := `func foo() {
	x := 1;
	y := 2;
	z := 3;
	return x + y + z;
}`
		result := Result{Body: body}
		truncated := TruncateBodySemantic(&result, 4)
		if !truncated {
			t.Error("should truncate")
		}
		if !strings.Contains(result.Body, "// ... truncated") {
			t.Error("should contain truncation marker")
		}
		lines := strings.Split(result.Body, "\n")
		lastContentLine := ""
		for i := len(lines) - 1; i >= 0; i-- {
			if !strings.Contains(lines[i], "truncated") && strings.TrimSpace(lines[i]) != "" {
				lastContentLine = strings.TrimSpace(lines[i])
				break
			}
		}
		if !strings.HasSuffix(lastContentLine, ";") && !strings.HasSuffix(lastContentLine, "{") {
			t.Errorf("should end at statement boundary, got %q", lastContentLine)
		}
	})

	t.Run("truncate at closing brace", func(t *testing.T) {
		body := `func foo() {
	if x > 0 {
		return x
	}
	return 0
}`
		result := Result{Body: body}
		truncated := TruncateBodySemantic(&result, 5)
		if !truncated {
			t.Error("should truncate")
		}
	})
}

func TestFindSemanticBoundary(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		maxLines int
		want     int
	}{
		{
			name:     "statement with semicolon",
			lines:    []string{"x := 1;", "y := 2;", "z := 3;"},
			maxLines: 2,
			want:     2,
		},
		{
			name:     "closing brace",
			lines:    []string{"if x {", "  return", "}", "next"},
			maxLines: 3,
			want:     3,
		},
		{
			name:     "opening brace",
			lines:    []string{"func foo() {", "  x := 1", "  return x"},
			maxLines: 2,
			want:     1,
		},
		{
			name:     "no good boundary",
			lines:    []string{"partial", "statement", "here"},
			maxLines: 2,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSemanticBoundary(tt.lines, tt.maxLines)
			if got != tt.want {
				t.Errorf("findSemanticBoundary() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTruncateResultsSemantic(t *testing.T) {
	results := []Result{
		{Name: "foo", File: "a.go", Match: "func foo()", Body: "func foo() {\n\treturn\n}"},
		{Name: "bar", File: "b.go", Match: "func bar()", Body: "func bar() {\n\treturn\n}"},
		{Name: "baz", File: "c.go", Match: "func baz()"},
	}

	t.Run("zero budget returns all", func(t *testing.T) {
		got, truncated, _ := TruncateResultsSemantic(results, 0, 10)
		if truncated {
			t.Error("should not truncate with zero budget")
		}
		if len(got) != len(results) {
			t.Errorf("got %d results, want %d", len(got), len(results))
		}
	})

	t.Run("large budget keeps all", func(t *testing.T) {
		got, truncated, tokens := TruncateResultsSemantic(results, 10000, 10)
		if truncated {
			t.Error("should not truncate with large budget")
		}
		if len(got) != len(results) {
			t.Errorf("got %d results, want %d", len(got), len(results))
		}
		if tokens == 0 {
			t.Error("tokens should be non-zero")
		}
	})

	t.Run("small budget truncates", func(t *testing.T) {
		got, truncated, _ := TruncateResultsSemantic(results, 300, 5)
		if !truncated {
			t.Error("should truncate with small budget")
		}
		if len(got) == 0 {
			t.Error("should return at least one result")
		}
	})
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
			minScore: 140,
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
			minScore: 90,
			maxScore: 110,
		},
		{
			name:     "contains match",
			result:   Result{Name: "ProcessOrder", Kind: "func", File: "a.go"},
			query:    "Order",
			minScore: 65,
			maxScore: 85,
		},
		{
			name:     "unexported lower score",
			result:   Result{Name: "processOrder", Kind: "func", File: "a.go"},
			query:    "processOrder",
			minScore: 120,
			maxScore: 140,
		},
		{
			name:     "reference kind lower score",
			result:   Result{Name: "ProcessOrder", Kind: "ref", File: "a.go"},
			query:    "ProcessOrder",
			minScore: 110,
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
		{Name: "handler", Kind: "func", File: "a.go"},
		{Name: "HandleRequest", Kind: "func", File: "b.go"},
		{Name: "handle", Kind: "func", File: "c.go"},
	}

	ScoreAndSort(results, "handle")

	if results[0].Name != "handle" {
		t.Errorf("first result should be exact match 'handle', got %q", results[0].Name)
	}

	if results[1].Name != "HandleRequest" {
		t.Errorf("second result should be prefix match 'HandleRequest', got %q", results[1].Name)
	}

	for i, r := range results {
		if r.Score == 0 {
			t.Errorf("result[%d] should have non-zero score", i)
		}
	}
}

func TestSuggestionsForDef(t *testing.T) {
	t.Run("nil result", func(t *testing.T) {
		suggestions := SuggestionsForDef(nil)
		if suggestions != nil {
			t.Error("should return nil for nil result")
		}
	})

	t.Run("function result", func(t *testing.T) {
		result := &Result{Name: "ProcessOrder", Kind: "func"}
		suggestions := SuggestionsForDef(result)

		if len(suggestions) < 3 {
			t.Errorf("expected at least 3 suggestions for func, got %d", len(suggestions))
		}

		// Should include refs, callers, callees suggestions
		hasRefs := false
		hasCallers := false
		hasCallees := false
		for _, s := range suggestions {
			if strings.Contains(s.Command, "refs") {
				hasRefs = true
			}
			if strings.Contains(s.Command, "callers") {
				hasCallers = true
			}
			if strings.Contains(s.Command, "callees") {
				hasCallees = true
			}
		}
		if !hasRefs {
			t.Error("should suggest refs command")
		}
		if !hasCallers {
			t.Error("should suggest callers command for func")
		}
		if !hasCallees {
			t.Error("should suggest callees command for func")
		}
	})

	t.Run("type result", func(t *testing.T) {
		result := &Result{Name: "Config", Kind: "type"}
		suggestions := SuggestionsForDef(result)

		// Should only suggest refs, not callers/callees
		if len(suggestions) != 1 {
			t.Errorf("expected 1 suggestion for type, got %d", len(suggestions))
		}
	})
}

func TestSuggestionsForRefs(t *testing.T) {
	t.Run("few results", func(t *testing.T) {
		suggestions := SuggestionsForRefs("foo", 5)
		if len(suggestions) < 1 {
			t.Error("should have at least one suggestion")
		}
	})

	t.Run("many results", func(t *testing.T) {
		suggestions := SuggestionsForRefs("foo", 50)
		hasSummary := false
		for _, s := range suggestions {
			if strings.Contains(s.Command, "--format=summary") {
				hasSummary = true
			}
		}
		if !hasSummary {
			t.Error("should suggest --format=summary for many results")
		}
	})
}

func TestSuggestionsForSearch(t *testing.T) {
	t.Run("no results", func(t *testing.T) {
		suggestions := SuggestionsForSearch("pattern", 0, false)
		if len(suggestions) == 0 {
			t.Error("should suggest alternatives when no results")
		}
		hasSim := false
		for _, s := range suggestions {
			if strings.Contains(s.Command, "snipe sim") {
				hasSim = true
			}
		}
		if !hasSim {
			t.Error("should suggest snipe sim when no results")
		}
	})

	t.Run("many results", func(t *testing.T) {
		suggestions := SuggestionsForSearch("TODO", 50, false)
		hasSummary := false
		for _, s := range suggestions {
			if strings.Contains(s.Command, "--format=summary") {
				hasSummary = true
			}
		}
		if !hasSummary {
			t.Error("should suggest --format=summary for many results")
		}
	})

	t.Run("semantic fallback used", func(t *testing.T) {
		suggestions := SuggestionsForSearch("handle request", 3, true)
		if len(suggestions) == 0 {
			t.Fatal("should suggest snipe sim when fallback used")
		}
		if !strings.Contains(suggestions[0].Command, "snipe sim") {
			t.Error("first suggestion should be snipe sim for fallback results")
		}
	})
}

func TestSuggestionsForAmbiguous(t *testing.T) {
	t.Run("empty candidates", func(t *testing.T) {
		suggestions := SuggestionsForAmbiguous(nil)
		if suggestions != nil {
			t.Error("should return nil for empty candidates")
		}
	})

	t.Run("multiple candidates", func(t *testing.T) {
		candidates := []Candidate{
			{ID: "abc123def45601", Name: "Config", File: "a/config.go", Kind: "type"},
			{ID: "abc123def45602", Name: "Config", File: "b/config.go", Kind: "type"},
		}
		suggestions := SuggestionsForAmbiguous(candidates)

		if len(suggestions) != 2 {
			t.Errorf("expected 2 suggestions, got %d", len(suggestions))
		}

		for _, s := range suggestions {
			if !strings.Contains(s.Command, "snipe show ") {
				t.Error("suggestions should use snipe show <id>")
			}
		}
	})

	t.Run("limits to 3 suggestions", func(t *testing.T) {
		candidates := []Candidate{
			{Name: "Config", File: "a/config.go", Kind: "type"},
			{Name: "Config", File: "b/config.go", Kind: "type"},
			{Name: "Config", File: "c/config.go", Kind: "type"},
			{Name: "Config", File: "d/config.go", Kind: "type"},
			{Name: "Config", File: "e/config.go", Kind: "type"},
		}
		suggestions := SuggestionsForAmbiguous(candidates)

		if len(suggestions) > 3 {
			t.Errorf("should limit to 3 suggestions, got %d", len(suggestions))
		}
	})
}

func TestSuggestionJSONFormat(t *testing.T) {
	resp := Response[Result]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results:  []Result{{Name: "foo", Kind: "func", File: "a.go"}},
		Meta:     Meta{Command: "def"},
		Suggestions: []Suggestion{
			{Command: "snipe refs foo", Description: "Find usages", Priority: 1},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	suggestions, ok := parsed["suggestions"].([]interface{})
	if !ok {
		t.Fatal("suggestions should be an array")
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion, got %d", len(suggestions))
	}

	s := suggestions[0].(map[string]interface{})
	if s["command"] != "snipe refs foo" {
		t.Errorf("command = %v, want 'snipe refs foo'", s["command"])
	}
	if s["description"] != "Find usages" {
		t.Errorf("description = %v, want 'Find usages'", s["description"])
	}
}
