package query_test

import (
	"path/filepath"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

func openLitTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestFindLiteralRefs(t *testing.T) {
	s := openLitTestStore(t)
	refs := []index.StringRef{
		{ID: "aabbccddeeff0011", Value: "MY_KEY", Kind: "env", FilePath: "/a.go", Line: 3, Col: 5, Snippet: `os.Getenv("MY_KEY")`},
		{ID: "1122334455667788", Value: "OTHER", Kind: "env", FilePath: "/b.go", Line: 7, Col: 2},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}

	got, err := query.FindLiteralRefs(s.DB(), "MY_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d: %+v", len(got), got)
	}
	if got[0].FilePath != "/a.go" || got[0].Line != 3 {
		t.Errorf("wrong result: %+v", got[0])
	}
	if got[0].Snippet != `os.Getenv("MY_KEY")` {
		t.Errorf("wrong snippet: %q", got[0].Snippet)
	}
}

func TestFindLiteralRefs_Missing(t *testing.T) {
	s := openLitTestStore(t)
	got, err := query.FindLiteralRefs(s.DB(), "NONEXISTENT")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}
