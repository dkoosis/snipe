package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

// SymbolKind represents the kind of symbol
type SymbolKind string

const (
	KindFunc      SymbolKind = "func"
	KindMethod    SymbolKind = "method"
	KindType      SymbolKind = "type"
	KindInterface SymbolKind = "interface"
	KindStruct    SymbolKind = "struct"
	KindVar       SymbolKind = "var"
	KindConst     SymbolKind = "const"
	KindField     SymbolKind = "field"
)

// Symbol represents a code symbol (function, type, etc.)
type Symbol struct {
	ID        string
	Name      string
	Kind      SymbolKind
	FilePath  string
	LineStart int // Display range start (includes 'func' keyword for functions)
	ColStart  int
	LineEnd   int
	ColEnd    int
	NameLine  int // Identifier position (used for ID generation and call graph linkage)
	NameCol   int
	Signature string
	Doc       string
	Receiver  string // For methods: "(*T)" or "(T)"
}

// ExtractSymbols extracts all symbols from loaded packages
func ExtractSymbols(result *LoadResult) ([]Symbol, error) {
	var symbols []Symbol

	for _, pkg := range result.Packages {
		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]

			fileSymbols := extractFileSymbols(pkg, file, filePath, result.Fset)
			symbols = append(symbols, fileSymbols...)
		}
	}

	return symbols, nil
}

func extractFileSymbols(pkg *packages.Package, file *ast.File, filePath string, fset *token.FileSet) []Symbol {
	var symbols []Symbol

	ast.Inspect(file, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			sym := extractFuncSymbol(pkg, decl, filePath, fset)
			if sym != nil {
				symbols = append(symbols, *sym)
			}

		case *ast.GenDecl:
			genSymbols := extractGenDeclSymbols(pkg, decl, filePath, fset)
			symbols = append(symbols, genSymbols...)
		}
		return true
	})

	return symbols
}

func extractFuncSymbol(_ *packages.Package, decl *ast.FuncDecl, filePath string, fset *token.FileSet) *Symbol {
	if decl.Name == nil {
		return nil
	}

	// Use decl.Name.Pos() for the identifier position - this matches what
	// types.Object.Pos() returns, enabling call graph linkage via posKey
	namePos := fset.Position(decl.Name.Pos())
	declPos := fset.Position(decl.Pos())
	endPos := fset.Position(decl.End())

	kind := KindFunc
	var receiver string

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		kind = KindMethod
		receiver = formatReceiver(decl.Recv.List[0].Type)
	}

	sig := formatFuncSignature(decl)
	doc := extractDoc(decl.Doc)

	return &Symbol{
		// ID uses identifier position for posKey matching with call graph
		ID:       generateID(filePath, namePos.Line, namePos.Column, string(kind)),
		Name:     decl.Name.Name,
		Kind:     kind,
		FilePath: filePath,
		// Range uses declaration start for user display (includes 'func' keyword)
		LineStart: declPos.Line,
		ColStart:  declPos.Column,
		LineEnd:   endPos.Line,
		ColEnd:    endPos.Column,
		// Identifier position for call graph linkage
		NameLine:  namePos.Line,
		NameCol:   namePos.Column,
		Signature: sig,
		Doc:       doc,
		Receiver:  receiver,
	}
}

func extractGenDeclSymbols(pkg *packages.Package, decl *ast.GenDecl, filePath string, fset *token.FileSet) []Symbol {
	var symbols []Symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sym := extractTypeSymbol(pkg, s, decl, filePath, fset)
			if sym != nil {
				symbols = append(symbols, *sym)
			}

		case *ast.ValueSpec:
			valSymbols := extractValueSymbols(pkg, s, decl, filePath, fset)
			symbols = append(symbols, valSymbols...)
		}
	}

	return symbols
}

func extractTypeSymbol(pkg *packages.Package, spec *ast.TypeSpec, decl *ast.GenDecl, filePath string, fset *token.FileSet) *Symbol {
	if spec.Name == nil {
		return nil
	}

	pos := fset.Position(spec.Pos())
	endPos := fset.Position(spec.End())

	kind := KindType
	switch spec.Type.(type) {
	case *ast.InterfaceType:
		kind = KindInterface
	case *ast.StructType:
		kind = KindStruct
	}

	// Get type info if available
	var sig string
	if pkg.TypesInfo != nil {
		if obj := pkg.TypesInfo.Defs[spec.Name]; obj != nil {
			sig = obj.Type().String()
		}
	}

	doc := extractDoc(decl.Doc)
	if doc == "" {
		doc = extractDoc(spec.Doc)
	}

	return &Symbol{
		ID:        generateID(filePath, pos.Line, pos.Column, string(kind)),
		Name:      spec.Name.Name,
		Kind:      kind,
		FilePath:  filePath,
		LineStart: pos.Line,
		ColStart:  pos.Column,
		LineEnd:   endPos.Line,
		ColEnd:    endPos.Column,
		NameLine:  pos.Line,
		NameCol:   pos.Column,
		Signature: sig,
		Doc:       doc,
	}
}

func extractValueSymbols(pkg *packages.Package, spec *ast.ValueSpec, decl *ast.GenDecl, filePath string, fset *token.FileSet) []Symbol {
	var symbols []Symbol

	kind := KindVar
	if decl.Tok == token.CONST {
		kind = KindConst
	}

	doc := extractDoc(decl.Doc)
	if doc == "" {
		doc = extractDoc(spec.Doc)
	}

	for _, name := range spec.Names {
		if name.Name == "_" {
			continue // Skip blank identifier
		}

		pos := fset.Position(name.Pos())
		endPos := fset.Position(name.End())

		var sig string
		if pkg.TypesInfo != nil {
			if obj := pkg.TypesInfo.Defs[name]; obj != nil {
				sig = obj.Type().String()
			}
		}

		symbols = append(symbols, Symbol{
			ID:        generateID(filePath, pos.Line, pos.Column, string(kind)),
			Name:      name.Name,
			Kind:      kind,
			FilePath:  filePath,
			LineStart: pos.Line,
			ColStart:  pos.Column,
			LineEnd:   endPos.Line,
			ColEnd:    endPos.Column,
			NameLine:  pos.Line,
			NameCol:   pos.Column,
			Signature: sig,
			Doc:       doc,
		})
	}

	return symbols
}

func formatReceiver(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		if ident, ok := t.X.(*ast.Ident); ok {
			return "(*" + ident.Name + ")"
		}
	case *ast.Ident:
		return "(" + t.Name + ")"
	}
	return ""
}

func formatFuncSignature(decl *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		b.WriteString(formatReceiver(decl.Recv.List[0].Type))
		b.WriteString(" ")
	}

	b.WriteString(decl.Name.Name)
	b.WriteString(formatFieldList(decl.Type.Params))

	if decl.Type.Results != nil {
		results := formatFieldList(decl.Type.Results)
		if strings.Contains(results, ",") || strings.Contains(results, " ") {
			b.WriteString(" ")
			b.WriteString(results)
		} else if results != "()" {
			b.WriteString(" ")
			b.WriteString(strings.Trim(results, "()"))
		}
	}

	return b.String()
}

func formatFieldList(fl *ast.FieldList) string {
	if fl == nil {
		return "()"
	}

	var parts []string
	for _, field := range fl.List {
		typeStr := types.ExprString(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typeStr)
		} else {
			for _, name := range field.Names {
				parts = append(parts, name.Name+" "+typeStr)
			}
		}
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

func extractDoc(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Text())
}

func generateID(filePath string, line, col int, kind string) string {
	data := fmt.Sprintf("%s:%d:%d:%s", filePath, line, col, kind)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes = 16 hex chars
}
