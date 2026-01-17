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
