package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"github.com/dkoosis/snipe/internal/util"
)

// StringRef is a reference to a string literal or env var name in source.
type StringRef struct {
	ID          string // 16-char hex
	Value       string // the string value, e.g. "SNIPE_VOYAGE_API_KEY"
	Name        string // for const refs: the declared identifier name
	Kind        string // "env" or "const"
	FilePath    string
	Line        int
	Col         int
	EnclosingID string // enclosing symbol ID (nullable)
	Snippet     string // source line for context
}

// ExtractLiterals extracts string literals from all packages in the result.
func ExtractLiterals(result *LoadResult, symbols []Symbol) []StringRef {
	encMap := buildGlobalEnclosingMap(result, symbols)
	var out []StringRef
	for _, pkg := range result.Packages {
		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]
			lines, err := util.LoadFileLines(filePath)
			refs := extractFileLiterals(file, filePath, result.Fset)
			for j := range refs {
				refs[j].ID = literalID(refs[j])
				if err == nil && refs[j].Line > 0 && refs[j].Line <= len(lines) {
					refs[j].Snippet = lines[refs[j].Line-1]
				}
				refs[j].EnclosingID = encMap[filePath][refs[j].Line]
			}
			out = append(out, refs...)
		}
	}
	return out
}

// ExtractLiteralsFiltered extracts only from files in the onlyFiles set.
func ExtractLiteralsFiltered(result *LoadResult, symbols []Symbol, onlyFiles map[string]bool) []StringRef {
	encMap := buildGlobalEnclosingMap(result, symbols)
	var out []StringRef
	for _, pkg := range result.Packages {
		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]
			if onlyFiles != nil && !onlyFiles[filePath] {
				continue
			}
			lines, err := util.LoadFileLines(filePath)
			refs := extractFileLiterals(file, filePath, result.Fset)
			for j := range refs {
				refs[j].ID = literalID(refs[j])
				if err == nil && refs[j].Line > 0 && refs[j].Line <= len(lines) {
					refs[j].Snippet = lines[refs[j].Line-1]
				}
				refs[j].EnclosingID = encMap[filePath][refs[j].Line]
			}
			out = append(out, refs...)
		}
	}
	return out
}

// extractFileLiterals walks a single file's AST extracting string literals.
func extractFileLiterals(file *ast.File, filePath string, fset *token.FileSet) []StringRef {
	var refs []StringRef
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			refs = append(refs, extractEnvCall(node, filePath, fset)...)
		case *ast.GenDecl:
			refs = append(refs, extractConstLiterals(node, filePath, fset)...)
		}
		return true
	})
	return refs
}

// extractEnvCall extracts os.Getenv / os.LookupEnv / os.Setenv calls.
func extractEnvCall(call *ast.CallExpr, filePath string, fset *token.FileSet) []StringRef {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "os" {
		return nil
	}
	switch sel.Sel.Name {
	case "Getenv", "LookupEnv", "Setenv":
	default:
		return nil
	}
	if len(call.Args) == 0 {
		return nil
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return nil
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return nil
	}
	pos := fset.Position(lit.Pos())
	return []StringRef{{
		Value:    value,
		Kind:     "env",
		FilePath: filePath,
		Line:     pos.Line,
		Col:      pos.Column,
	}}
}

// extractConstLiterals extracts named string constants.
func extractConstLiterals(decl *ast.GenDecl, filePath string, fset *token.FileSet) []StringRef {
	if decl.Tok != token.CONST {
		return nil
	}
	var refs []StringRef
	for _, spec := range decl.Specs {
		vspec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, val := range vspec.Values {
			lit, ok := val.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			name := ""
			if i < len(vspec.Names) {
				name = vspec.Names[i].Name
			}
			pos := fset.Position(lit.Pos())
			refs = append(refs, StringRef{
				Value:    value,
				Name:     name,
				Kind:     "const",
				FilePath: filePath,
				Line:     pos.Line,
				Col:      pos.Column,
			})
		}
	}
	return refs
}

// literalID generates a stable 16-char hex ID for a string ref.
func literalID(r StringRef) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", r.FilePath, r.Line, r.Col, r.Value)))
	return hex.EncodeToString(h[:])[:16]
}

// buildGlobalEnclosingMap returns map[filePath][line] -> enclosing symbol ID.
func buildGlobalEnclosingMap(_ *LoadResult, symbols []Symbol) map[string]map[int]string {
	m := make(map[string]map[int]string)
	for i := range symbols {
		sym := &symbols[i]
		if sym.Kind != KindFunc && sym.Kind != KindMethod {
			continue
		}
		if m[sym.FilePath] == nil {
			m[sym.FilePath] = make(map[int]string)
		}
		for line := sym.LineStart; line <= sym.LineEnd; line++ {
			m[sym.FilePath][line] = sym.ID
		}
	}
	return m
}
