package index

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"
)

// Ref represents a reference to a symbol
type Ref struct {
	ID          string
	SymbolID    string // ID of the referenced symbol
	FilePath    string
	Line        int
	Col         int
	EnclosingID string // ID of the enclosing function/method
	Snippet     string // The line of code
}

// ExtractRefs extracts all references from loaded packages
func ExtractRefs(result *LoadResult, symbols []Symbol) ([]Ref, error) {
	// Build symbol lookup by definition position
	symbolByPos := buildSymbolPosIndex(symbols)

	var refs []Ref

	for _, pkg := range result.Packages {
		if pkg.TypesInfo == nil {
			continue
		}

		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]

			// Load file content for snippets
			lines, err := loadFileLines(filePath)
			if err != nil {
				continue // Skip files we can't read
			}

			// Build enclosing function map for this file
			enclosingMap := buildEnclosingMap(file, filePath, result.Fset)

			// Extract references from Uses map
			for ident, obj := range pkg.TypesInfo.Uses {
				if obj == nil {
					continue
				}

				// Get the definition position
				defPos := obj.Pos()
				if !defPos.IsValid() {
					continue
				}

				defPosInfo := result.Fset.Position(defPos)
				defKey := posKey(defPosInfo.Filename, defPosInfo.Line, defPosInfo.Column)

				// Look up the symbol ID
				symbolID, ok := symbolByPos[defKey]
				if !ok {
					continue // Reference to symbol not in our index (e.g., stdlib)
				}

				// Get reference position
				refPos := result.Fset.Position(ident.Pos())
				if refPos.Filename != filePath {
					continue // Skip if not in current file
				}

				// Get enclosing function
				enclosingID := findEnclosing(ident.Pos(), enclosingMap)

				// Get snippet
				snippet := ""
				if refPos.Line > 0 && refPos.Line <= len(lines) {
					snippet = strings.TrimSpace(lines[refPos.Line-1])
				}

				ref := Ref{
					ID:          generateID(filePath, refPos.Line, refPos.Column, "ref"),
					SymbolID:    symbolID,
					FilePath:    filePath,
					Line:        refPos.Line,
					Col:         refPos.Column,
					EnclosingID: enclosingID,
					Snippet:     snippet,
				}
				refs = append(refs, ref)
			}
		}
	}

	return refs, nil
}

// buildSymbolPosIndex creates a map from position key to symbol ID
// Uses NameLine/NameCol (identifier position) to match obj.Pos() in call graph lookups
func buildSymbolPosIndex(symbols []Symbol) map[string]string {
	index := make(map[string]string)
	for _, sym := range symbols {
		key := posKey(sym.FilePath, sym.NameLine, sym.NameCol)
		index[key] = sym.ID
	}
	return index
}

func posKey(file string, line, col int) string {
	return fmt.Sprintf("%s:%d:%d", file, line, col)
}

// enclosingFunc tracks function/method ranges for finding enclosing scope
type enclosingFunc struct {
	id    string
	start token.Pos
	end   token.Pos
}

func buildEnclosingMap(file *ast.File, filePath string, fset *token.FileSet) []enclosingFunc {
	var funcs []enclosingFunc

	ast.Inspect(file, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok && fn.Body != nil && fn.Name != nil {
			// Use fn.Name.Pos() to match symbol ID generation in symbols.go
			namePos := fset.Position(fn.Name.Pos())
			kind := KindFunc
			if fn.Recv != nil {
				kind = KindMethod
			}
			funcs = append(funcs, enclosingFunc{
				id:    generateID(filePath, namePos.Line, namePos.Column, string(kind)),
				start: fn.Body.Lbrace,
				end:   fn.Body.Rbrace,
			})
		}
		return true
	})

	return funcs
}

func findEnclosing(pos token.Pos, funcs []enclosingFunc) string {
	for _, fn := range funcs {
		if pos >= fn.start && pos <= fn.end {
			return fn.id
		}
	}
	return ""
}

func loadFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	// Increase buffer for long lines (minified code, long strings)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
