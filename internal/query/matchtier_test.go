package query

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

// TestMatchTier_SimpleLadder: an exact name hit carries no tier (served); a
// name that only resolves case-insensitively carries MatchCaseInsens. This is
// the self-assessment signal Phase 3 emits — see snipe-ffj.
func TestMatchTier_SimpleLadder(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, ".snipe", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.DB().Exec(`INSERT INTO symbols (id, name, kind, file_path, line_start, col_start, line_end, col_end)
		VALUES ('sym1', 'FooBar', 'func', 'main.go', 1, 1, 1, 1)`); err != nil {
		t.Fatalf("seed symbol: %v", err)
	}

	// Exact match: served, no tier.
	exact, err := lookupSimple(s.DB(), "FooBar")
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 {
		t.Fatalf("exact: want 1 row, got %d", len(exact))
	}
	if exact[0].MatchTier != MatchExact {
		t.Errorf("exact: want MatchExact (empty), got %q", exact[0].MatchTier)
	}

	// Case-insensitive fallback: degraded, MatchCaseInsens.
	ci, err := lookupSimple(s.DB(), "foobar")
	if err != nil {
		t.Fatal(err)
	}
	if len(ci) != 1 {
		t.Fatalf("ci: want 1 row, got %d", len(ci))
	}
	if ci[0].MatchTier != MatchCaseInsens {
		t.Errorf("ci: want MatchCaseInsens, got %q", ci[0].MatchTier)
	}
}
