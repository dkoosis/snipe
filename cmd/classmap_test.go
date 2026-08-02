package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

// parseTypeSpecForTest parses src and returns the *ast.TypeSpec named
// typeName, plus the source bytes and fset needed to slice it — the same
// inputs fillFieldList/sourceSlice take in the real parseClassMembers path.
func parseTypeSpecForTest(t *testing.T, src, typeName string) (*ast.TypeSpec, []byte, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if ok && ts.Name.Name == typeName {
				return ts, []byte(src), fset
			}
		}
	}
	t.Fatalf("type %s not found in source", typeName)
	return nil, nil, nil
}

func TestFillFieldList_StructFieldsAndEmbeds(t *testing.T) {
	src := `package p

type Widget struct {
	Base
	*sync.Mutex
	Label string
	count int
	Data  map[string]int
}
`
	ts, src2, fset := parseTypeSpecForTest(t, src, "Widget")
	st, ok := ts.Type.(*ast.StructType)
	if !ok {
		t.Fatalf("Widget is not a struct")
	}

	ct := &classType{}
	fillFieldList(ct, st.Fields, src2, fset, false)

	wantEmbeds := []string{"Base", "*sync.Mutex"}
	if len(ct.Embeds) != len(wantEmbeds) {
		t.Fatalf("embeds = %v, want %v", ct.Embeds, wantEmbeds)
	}
	for i, e := range wantEmbeds {
		if ct.Embeds[i] != e {
			t.Errorf("embeds[%d] = %q, want %q", i, ct.Embeds[i], e)
		}
	}

	wantFields := map[string]struct {
		typ      string
		exported bool
	}{
		"Label": {"string", true},
		"count": {"int", false},
		"Data":  {"map[string]int", true},
	}
	if len(ct.Fields) != len(wantFields) {
		t.Fatalf("fields = %+v, want %d entries", ct.Fields, len(wantFields))
	}
	for _, f := range ct.Fields {
		want, ok := wantFields[f.Name]
		if !ok {
			t.Errorf("unexpected field %q", f.Name)
			continue
		}
		if f.Type != want.typ {
			t.Errorf("field %s type = %q, want %q", f.Name, f.Type, want.typ)
		}
		if f.Exported != want.exported {
			t.Errorf("field %s exported = %v, want %v", f.Name, f.Exported, want.exported)
		}
	}
}

func TestFillFieldList_InterfaceMethodsAndEmbeds(t *testing.T) {
	src := `package p

type Namer interface {
	io.Closer
	Name() string
	Set(key string, value int) error
}
`
	ts, src2, fset := parseTypeSpecForTest(t, src, "Namer")
	it, ok := ts.Type.(*ast.InterfaceType)
	if !ok {
		t.Fatalf("Namer is not an interface")
	}

	ct := &classType{}
	fillFieldList(ct, it.Methods, src2, fset, true)

	if len(ct.Embeds) != 1 || ct.Embeds[0] != "io.Closer" {
		t.Errorf("embeds = %v, want [io.Closer]", ct.Embeds)
	}
	if len(ct.Methods) != 2 {
		t.Fatalf("methods = %+v, want 2 entries", ct.Methods)
	}
	if ct.Methods[0].Name != "Name" || ct.Methods[0].Type != "() string" {
		t.Errorf("methods[0] = %+v, want Name() string", ct.Methods[0])
	}
	if ct.Methods[1].Name != "Set" || ct.Methods[1].Type != "(key string, value int) error" {
		t.Errorf("methods[1] = %+v", ct.Methods[1])
	}
}

func TestMethodSignatureTail(t *testing.T) {
	tests := []struct {
		sig, name, want string
	}{
		{"func (s *Store) Close() error", "Close", "() error"},
		{"func (s *Store) Get(key string) (int, bool)", "Get", "(key string) (int, bool)"},
		{"", "Close", "()"},
	}
	for _, tt := range tests {
		if got := methodSignatureTail(tt.sig, tt.name); got != tt.want {
			t.Errorf("methodSignatureTail(%q, %q) = %q, want %q", tt.sig, tt.name, got, tt.want)
		}
	}
}

func TestFieldMemberLine(t *testing.T) {
	tests := []struct {
		m    classMember
		want string
	}{
		{classMember{Name: "Label", Type: "string", Exported: true}, "+Label: string"},
		{classMember{Name: "count", Type: "int", Exported: false}, "-count: int"},
		{classMember{Name: "Close", Type: "() error", Exported: true}, "+Close() error"},
	}
	for _, tt := range tests {
		if got := fieldMemberLine(tt.m); got != tt.want {
			t.Errorf("fieldMemberLine(%+v) = %q, want %q", tt.m, got, tt.want)
		}
	}
}

func TestRankClassTypes_OrderingAndCap(t *testing.T) {
	mk := func(name string, fields, methods, embeds int) *classType {
		ct := &classType{Sym: query.SymbolRow{Name: name}}
		for range fields {
			ct.Fields = append(ct.Fields, classMember{})
		}
		for range methods {
			ct.Methods = append(ct.Methods, classMember{})
		}
		for range embeds {
			ct.Embeds = append(ct.Embeds, "x")
		}
		return ct
	}

	types := []*classType{
		mk("Small", 1, 0, 0),
		mk("Big", 5, 2, 1),
		mk("TieA", 2, 0, 0),
		mk("TieB", 2, 0, 0),
	}

	ranked := rankClassTypes(types, 0)
	wantOrder := []string{"Big", "TieA", "TieB", "Small"}
	if len(ranked) != len(wantOrder) {
		t.Fatalf("ranked = %d entries, want %d", len(ranked), len(wantOrder))
	}
	for i, name := range wantOrder {
		if ranked[i].Sym.Name != name {
			t.Errorf("ranked[%d] = %s, want %s", i, ranked[i].Sym.Name, name)
		}
	}

	capped := rankClassTypes(types, 2)
	if len(capped) != 2 {
		t.Fatalf("capped = %d entries, want 2", len(capped))
	}
	if capped[0].Sym.Name != "Big" || capped[1].Sym.Name != "TieA" {
		t.Errorf("capped = [%s, %s], want [Big, TieA]", capped[0].Sym.Name, capped[1].Sym.Name)
	}
}

func TestClassNodeID_QualifiesByPackage(t *testing.T) {
	a := classNodeID(query.SymbolRow{Name: "Store", PkgPath: "github.com/x/a"})
	b := classNodeID(query.SymbolRow{Name: "Store", PkgPath: "github.com/x/b"})
	if a == b {
		t.Errorf("classNodeID collided for same-named types in different packages: %q", a)
	}
}
