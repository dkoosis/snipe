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

func TestFindLiteralsByKind(t *testing.T) {
	s := openLitTestStore(t)
	refs := []index.StringRef{
		{ID: "aabbccddeeff0011", Value: "VOYAGE_API_URL", Kind: "env", FilePath: "/b.go", Line: 3, Col: 5},
		{ID: "1122334455667788", Value: "fixtureConst", Name: "fixtureConst", Kind: "const", FilePath: "/a.go", Line: 1, Col: 1},
		{ID: "2233445566778899", Value: "SNIPE_VOYAGE_API_KEY", Kind: "env", FilePath: "/a.go", Line: 7, Col: 2},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}

	got, err := query.FindLiteralsByKind(s.DB(), "env")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 env refs, got %d: %+v", len(got), got)
	}
	// ORDER BY file_path, line, col: /a.go:7 sorts before /b.go:3.
	if got[0].FilePath != "/a.go" || got[0].Value != "SNIPE_VOYAGE_API_KEY" {
		t.Errorf("index 0: unexpected %+v", got[0])
	}
	if got[1].FilePath != "/b.go" || got[1].Value != "VOYAGE_API_URL" {
		t.Errorf("index 1: unexpected %+v", got[1])
	}
}

func TestFindLiteralsByKind_Empty(t *testing.T) {
	s := openLitTestStore(t)
	got, err := query.FindLiteralsByKind(s.DB(), "env")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}

// churnFixture seeds string_refs covering both predicate arms (testdata/ and
// .golden), a dup across two covering test files, a non-matching literal, and a
// match in a NON-covering file — so a single fixture exercises scoping, the two
// arms, dedup, and exclusion.
func churnFixture(t *testing.T) *store.Store {
	t.Helper()
	s := openLitTestStore(t)
	refs := []index.StringRef{
		{ID: "aa00000000000001", Value: "testdata/foo.golden", Kind: "const", FilePath: "/pkg/foo_test.go", Line: 3, Col: 2},
		{ID: "aa00000000000002", Value: "snapshots/out.golden", Kind: "const", FilePath: "/pkg/foo_test.go", Line: 4, Col: 2},
		{ID: "aa00000000000003", Value: "testdata/shared.golden", Kind: "const", FilePath: "/pkg/foo_test.go", Line: 5, Col: 2},
		{ID: "aa00000000000004", Value: "testdata/shared.golden", Kind: "const", FilePath: "/pkg/a_test.go", Line: 9, Col: 2},
		{ID: "aa00000000000005", Value: "SOME_ENV", Kind: "env", FilePath: "/pkg/foo_test.go", Line: 6, Col: 2},
		{ID: "aa00000000000006", Value: "testdata/other.golden", Kind: "const", FilePath: "/pkg/bar_test.go", Line: 2, Col: 2},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFindChurnLiterals(t *testing.T) {
	s := churnFixture(t)
	got, truncated, err := query.FindChurnLiterals(s.DB(), []string{"/pkg/foo_test.go", "/pkg/a_test.go"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	// value-sorted, deduped across the two covering files; the non-covering
	// bar_test.go match and the env literal are excluded.
	want := []string{"snapshots/out.golden", "testdata/foo.golden", "testdata/shared.golden"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q (full: %v)", i, want[i], got[i], got)
		}
	}
	if truncated != 0 {
		t.Errorf("want truncated 0, got %d", truncated)
	}
}

func TestFindChurnLiterals_Cap(t *testing.T) {
	s := churnFixture(t)
	got, truncated, err := query.FindChurnLiterals(s.DB(), []string{"/pkg/foo_test.go", "/pkg/a_test.go"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	// Cap keeps the first two by sort order; the third is dropped and counted.
	want := []string{"snapshots/out.golden", "testdata/foo.golden"}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: want %q, got %q", i, want[i], got[i])
		}
	}
	if truncated != 1 {
		t.Errorf("want truncated 1, got %d", truncated)
	}
}

func TestFindChurnLiterals_Empty(t *testing.T) {
	s := churnFixture(t)
	got, truncated, err := query.FindChurnLiterals(s.DB(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil || truncated != 0 {
		t.Errorf("want (nil,0), got (%v,%d)", got, truncated)
	}
}
