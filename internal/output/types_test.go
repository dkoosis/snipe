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
