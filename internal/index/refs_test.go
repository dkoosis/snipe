package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPosKey(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		line     int
		col      int
		expected string
	}{
		{
			name:     "simple case",
			file:     "/path/to/file.go",
			line:     10,
			col:      5,
			expected: "/path/to/file.go:10:5",
		},
		{
			name:     "multi-digit line and col",
			file:     "/path/to/file.go",
			line:     300,
			col:      42,
			expected: "/path/to/file.go:300:42",
		},
		{
			name:     "large line numbers",
			file:     "/path/to/file.go",
			line:     10000,
			col:      999,
			expected: "/path/to/file.go:10000:999",
		},
		{
			name:     "line 1 col 1",
			file:     "main.go",
			line:     1,
			col:      1,
			expected: "main.go:1:1",
		},
		{
			name:     "windows path with colon",
			file:     "C:/Users/test/file.go",
			line:     25,
			col:      10,
			expected: "C:/Users/test/file.go:25:10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := posKey(tt.file, tt.line, tt.col)
			if got != tt.expected {
				t.Errorf("posKey(%q, %d, %d) = %q, want %q",
					tt.file, tt.line, tt.col, got, tt.expected)
			}
		})
	}
}

func TestPosKeyDeterminism(t *testing.T) {
	// Same inputs should always produce the same output
	file := "/path/to/file.go"
	line := 300
	col := 42

	key1 := posKey(file, line, col)
	key2 := posKey(file, line, col)

	if key1 != key2 {
		t.Errorf("posKey is not deterministic: %q != %q", key1, key2)
	}
}

func TestPosKeyUniqueness(t *testing.T) {
	// Different inputs should produce different keys
	keys := make(map[string]bool)

	testCases := []struct {
		file string
		line int
		col  int
	}{
		{"/file.go", 1, 1},
		{"/file.go", 1, 2},
		{"/file.go", 2, 1},
		{"/other.go", 1, 1},
		{"/file.go", 11, 1}, // Should not collide with line 1, col 1
		{"/file.go", 1, 11}, // Should not collide with line 1, col 1
	}

	for _, tc := range testCases {
		key := posKey(tc.file, tc.line, tc.col)
		if keys[key] {
			t.Errorf("duplicate key generated: %s for (%s, %d, %d)",
				key, tc.file, tc.line, tc.col)
		}
		keys[key] = true
	}
}

func TestBuildSymbolPosIndex(t *testing.T) {
	symbols := []Symbol{
		{ID: "sym1", FilePath: "/file.go", NameLine: 10, NameCol: 5, Kind: KindFunc},
		{ID: "sym2", FilePath: "/file.go", NameLine: 20, NameCol: 10, Kind: KindFunc},
		{ID: "sym3", FilePath: "/other.go", NameLine: 10, NameCol: 5, Kind: KindFunc},
	}

	index := buildSymbolPosIndex(symbols)

	tests := []struct {
		file   string
		line   int
		col    int
		wantID string
		wantOK bool
	}{
		{"/file.go", 10, 5, "sym1", true},
		{"/file.go", 20, 10, "sym2", true},
		{"/other.go", 10, 5, "sym3", true},
		{"/file.go", 10, 6, "", false},    // wrong col
		{"/file.go", 11, 5, "", false},    // wrong line
		{"/missing.go", 10, 5, "", false}, // wrong file
		// Fallback tests for col 1 (chunked loading)
		{"/file.go", 10, 1, "sym1", true}, // fallback matches sym1
		{"/file.go", 20, 1, "sym2", true}, // fallback matches sym2
	}

	for _, tt := range tests {
		gotID, gotOK := index.Lookup(tt.file, tt.line, tt.col)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("Lookup(%s, %d, %d) = (%q, %v), want (%q, %v)",
				tt.file, tt.line, tt.col, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}

// TestExtractRefs_SignatureRefsGetEnclosingID verifies refs in a function
// signature (return type, params) are attributed to that function at index
// time — previously they were orphaned (enclosing range started at the body
// Lbrace) and recovered at the command layer.
func TestExtractRefs_SignatureRefsGetEnclosingID(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/sig\n\ngo 1.22\n",
		"sig.go": `package sig

type Widget struct{}

func MakeWidget(n int) *Widget {
	return &Widget{}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	symbols, err := ExtractSymbols(result)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	refs, err := ExtractRefs(result, symbols)
	if err != nil {
		t.Fatalf("extract refs: %v", err)
	}

	var makeWidgetID string
	for _, s := range symbols {
		if s.Name == "MakeWidget" {
			makeWidgetID = s.ID
		}
	}
	if makeWidgetID == "" {
		t.Fatal("MakeWidget symbol not found")
	}

	// The Widget ref on line 5 is in MakeWidget's signature (return type).
	var found bool
	for _, r := range refs {
		if r.Line == 5 {
			found = true
			if r.EnclosingID != makeWidgetID {
				t.Errorf("signature ref on line %d has enclosing_id %q, want MakeWidget %q",
					r.Line, r.EnclosingID, makeWidgetID)
			}
		}
	}
	if !found {
		t.Fatal("no ref found on MakeWidget's signature line")
	}
}
