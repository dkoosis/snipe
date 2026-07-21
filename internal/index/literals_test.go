package index

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestExtractLiterals_Env(t *testing.T) {
	src := `package p
import "os"
func f() {
	_ = os.Getenv("MY_KEY")
	_, _ = os.LookupEnv("OTHER_KEY")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	refs := extractFileLiterals(file, "f.go", fset)
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].Value != "MY_KEY" || refs[0].Kind != "env" {
		t.Errorf("first ref: got %+v", refs[0])
	}
	if refs[1].Value != "OTHER_KEY" || refs[1].Kind != "env" {
		t.Errorf("second ref: got %+v", refs[1])
	}
}

func TestExtractLiterals_Const(t *testing.T) {
	src := `package p
const (
	KeyA = "hello"
	KeyB = "world"
)
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	refs := extractFileLiterals(file, "f.go", fset)
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %d", len(refs))
	}
	if refs[0].Value != "hello" || refs[0].Kind != "const" || refs[0].Name != "KeyA" {
		t.Errorf("first const: got %+v", refs[0])
	}
}

// TestExtractLiterals_InlineFixture covers the sn-8f6q.9 broadening: an inline
// golden-path argument (no const, no env) is indexed as kind=fixture so `snipe
// plan`'s churn detector fires on it. Both predicate arms are exercised — a
// "testdata/" substring and a ".golden" suffix.
func TestExtractLiterals_InlineFixture(t *testing.T) {
	src := `package p
func f(t T) {
	golden.Assert(t, got, "testdata/x.golden")
	os.ReadFile("fixtures/out.golden")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	refs := extractFileLiterals(file, "f.go", fset)
	if len(refs) != 2 {
		t.Fatalf("want 2 fixture refs, got %d: %+v", len(refs), refs)
	}
	for _, r := range refs {
		if r.Kind != "fixture" {
			t.Errorf("want kind=fixture, got %+v", r)
		}
	}
	got := map[string]bool{refs[0].Value: true, refs[1].Value: true}
	for _, want := range []string{"testdata/x.golden", "fixtures/out.golden"} {
		if !got[want] {
			t.Errorf("missing fixture value %q (got %v)", want, got)
		}
	}
}

// TestExtractLiterals_InlineNonFixtureIgnored is the bound: an ordinary inline
// string literal that does NOT match the fixture predicate must NOT be indexed.
// This is what keeps the broadening from ballooning string_refs repo-wide.
func TestExtractLiterals_InlineNonFixtureIgnored(t *testing.T) {
	src := `package p
func f() {
	fmt.Println("hello world")
	log.Print("some/other/path.txt")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	refs := extractFileLiterals(file, "f.go", fset)
	if len(refs) != 0 {
		t.Fatalf("want 0 refs for non-fixture inline literals, got %d: %+v", len(refs), refs)
	}
}

// TestExtractLiterals_FixtureConstNotDoubleCounted proves dedup: a const whose
// value happens to match the fixture predicate stays a single kind=const row
// (never also emitted as kind=fixture). Churn still finds it — churn keys on the
// value, not the kind.
func TestExtractLiterals_FixtureConstNotDoubleCounted(t *testing.T) {
	src := `package p
const golden = "testdata/x.golden"
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "f.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	refs := extractFileLiterals(file, "f.go", fset)
	if len(refs) != 1 {
		t.Fatalf("want exactly 1 ref (const, not double-counted), got %d: %+v", len(refs), refs)
	}
	if refs[0].Kind != "const" || refs[0].Name != "golden" || refs[0].Value != "testdata/x.golden" {
		t.Errorf("want single const ref, got %+v", refs[0])
	}
}
