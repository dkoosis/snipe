package query

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1, s2 string
		want   int
	}{
		// Identical strings
		{"", "", 0},
		{"a", "a", 0},
		{"hello", "hello", 0},

		// Empty string cases
		{"", "abc", 3},
		{"abc", "", 3},

		// Single character changes
		{"cat", "hat", 1},  // substitution
		{"cat", "cats", 1}, // insertion
		{"cats", "cat", 1}, // deletion

		// Multiple changes
		{"kitten", "sitting", 3},
		{"saturday", "sunday", 3},

		// Case insensitivity
		{"Hello", "hello", 0},
		{"WORLD", "world", 0},
		{"GoLang", "golang", 0},

		// Common typos
		{"handler", "Handler", 0},
		{"hanlder", "handler", 2}, // transposition = 2 edits
		{"hnadler", "handler", 2},
		{"proces", "process", 1},
		{"proccess", "process", 1},

		// Symbol name variations
		{"ProcessOrder", "processorder", 0},
		{"ProcessOrdr", "ProcessOrder", 1},
		{"ProccessOrder", "ProcessOrder", 1},
	}

	for _, tt := range tests {
		t.Run(tt.s1+"_"+tt.s2, func(t *testing.T) {
			got := LevenshteinDistance(tt.s1, tt.s2)
			if got != tt.want {
				t.Errorf("LevenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
			}
		})
	}
}

func TestDefaultMaxDistance(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"ab", 1},
		{"abc", 1},
		{"abcd", 2},
		{"abcdef", 2},
		{"abcdefg", 3},
		{"abcdefghijkl", 3},
		{"abcdefghijklm", 4},
		{"abcdefghijklmnop", 4},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := DefaultMaxDistance(tt.query)
			if got != tt.want {
				t.Errorf("DefaultMaxDistance(%q) = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}

func TestFindSimilarSymbols(t *testing.T) {
	// Create an in-memory SQLite database for testing
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create the symbols table
	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT,
			file_path TEXT,
			file_path_rel TEXT,
			line_start INTEGER,
			col_start INTEGER,
			line_end INTEGER,
			col_end INTEGER,
			signature TEXT,
			doc TEXT,
			receiver TEXT
		);
		CREATE INDEX idx_symbols_name ON symbols(name);
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert test symbols
	symbols := []string{
		"ProcessOrder",
		"ProcessPayment",
		"ProcessRefund",
		"HandleRequest",
		"HandleResponse",
		"ValidateInput",
		"ValidateOutput",
		"CreateUser",
		"DeleteUser",
		"UpdateUser",
	}

	for i, name := range symbols {
		_, err = db.Exec(`
			INSERT INTO symbols (id, name, kind, file_path, line_start, col_start, line_end, col_end)
			VALUES (?, ?, 'func', 'test.go', 1, 1, 10, 1)
		`, i, name)
		if err != nil {
			t.Fatalf("Failed to insert symbol: %v", err)
		}
	}

	tests := []struct {
		name        string
		query       string
		maxDist     int
		maxResults  int
		wantContain []string
		wantExclude []string
	}{
		{
			name:        "typo in ProcessOrder",
			query:       "ProccessOrder",
			maxDist:     2,
			maxResults:  3,
			wantContain: []string{"ProcessOrder"},
		},
		{
			name:        "missing letter",
			query:       "ProcessOrdr",
			maxDist:     2,
			maxResults:  3,
			wantContain: []string{"ProcessOrder"},
		},
		{
			name:        "partial match Process",
			query:       "Process",
			maxDist:     3,
			maxResults:  5,
			wantContain: []string{}, // "Process" is a prefix, not within edit distance of full names
		},
		{
			name:        "typo in Handle",
			query:       "Hanle",
			maxDist:     2,
			maxResults:  3,
			wantContain: []string{}, // Edit distance to HandleRequest/HandleResponse is > 2
		},
		{
			name:        "close match CreateUser",
			query:       "CreatUser",
			maxDist:     2,
			maxResults:  3,
			wantContain: []string{"CreateUser"},
		},
		{
			name:       "no matches for completely different",
			query:      "XyzAbc",
			maxDist:    2,
			maxResults: 3,
			// Should return nothing as no symbols are within distance 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindSimilarSymbols(db, tt.query, tt.maxDist, tt.maxResults)
			if err != nil {
				t.Fatalf("FindSimilarSymbols() error = %v", err)
			}

			// Check that expected symbols are present
			for _, want := range tt.wantContain {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("FindSimilarSymbols() missing expected %q, got %v", want, got)
				}
			}

			// Check that excluded symbols are not present
			for _, exclude := range tt.wantExclude {
				for _, g := range got {
					if g == exclude {
						t.Errorf("FindSimilarSymbols() should not contain %q, got %v", exclude, got)
					}
				}
			}

			// Check max results limit
			if len(got) > tt.maxResults {
				t.Errorf("FindSimilarSymbols() returned %d results, want at most %d", len(got), tt.maxResults)
			}
		})
	}
}

func TestFindSimilarSymbols_SortedByDistance(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT,
			file_path TEXT,
			file_path_rel TEXT,
			line_start INTEGER,
			col_start INTEGER,
			line_end INTEGER,
			col_end INTEGER,
			signature TEXT,
			doc TEXT,
			receiver TEXT
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Insert symbols with varying distances from "Handler"
	symbols := []string{
		"Handler",   // distance 0 (exact, should be excluded)
		"Handlers",  // distance 1
		"HandlerX",  // distance 1
		"Handlery",  // distance 1
		"Hndler",    // distance 1
		"Handlerss", // distance 2
		"Haandler",  // distance 1
	}

	for i, name := range symbols {
		_, err = db.Exec(`
			INSERT INTO symbols (id, name, kind, file_path, line_start, col_start, line_end, col_end)
			VALUES (?, ?, 'func', 'test.go', 1, 1, 10, 1)
		`, i, name)
		if err != nil {
			t.Fatalf("Failed to insert symbol: %v", err)
		}
	}

	got, err := FindSimilarSymbols(db, "Handler", 2, 10)
	if err != nil {
		t.Fatalf("FindSimilarSymbols() error = %v", err)
	}

	// Should not include exact match
	for _, g := range got {
		if g == "Handler" {
			t.Errorf("FindSimilarSymbols() should not include exact match 'Handler'")
		}
	}

	// Verify results are sorted by distance
	prevDist := 0
	for _, g := range got {
		dist := LevenshteinDistance("Handler", g)
		if dist < prevDist {
			t.Errorf("Results not sorted by distance: %v", got)
			break
		}
		prevDist = dist
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
