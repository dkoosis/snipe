package index

import "testing"

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
		{ID: "sym1", FilePath: "/file.go", NameLine: 10, NameCol: 5},
		{ID: "sym2", FilePath: "/file.go", NameLine: 20, NameCol: 10},
		{ID: "sym3", FilePath: "/other.go", NameLine: 10, NameCol: 5},
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
	}

	for _, tt := range tests {
		key := posKey(tt.file, tt.line, tt.col)
		gotID, gotOK := index[key]
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("index[%s] = (%q, %v), want (%q, %v)",
				key, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}
