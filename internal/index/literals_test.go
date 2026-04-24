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
