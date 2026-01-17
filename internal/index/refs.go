package index

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/dkoosis/snipe/internal/util"
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

// ExtractRefs extracts all references from loaded packages.
// Uses an optimized approach: single AST pass with function stack tracking
// for O(1) enclosing function lookup per reference.
func ExtractRefs(result *LoadResult, symbols []Symbol) ([]Ref, error) {
	return ExtractRefsWithCache(result, symbols, nil)
}

// ExtractRefsWithCache extracts references using an optional file cache.
// If cache is nil, files are loaded without caching.
func ExtractRefsWithCache(result *LoadResult, symbols []Symbol, cache *util.FileCache) ([]Ref, error) {
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

			// Load file content for snippets (with optional cache)
			var lines []string
			var err error
			if cache != nil {
				lines, err = cache.LoadLines(filePath)
			} else {
				lines, err = util.LoadFileLines(filePath)
			}
			if err != nil {
				continue // Skip files we can't read
			}

			// Build position-to-enclosing map using AST walker with function stack.
			// This is O(N) single pass instead of binary search per reference.
			enclosingByPos := buildEnclosingByPos(file, filePath, result.Fset)

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

				// Get enclosing function - O(1) map lookup
				enclosingID := enclosingByPos[ident.Pos()]

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

// buildEnclosingByPos creates a map from AST positions to enclosing function IDs.
// Uses a single AST traversal with a function stack for O(1) lookup per position.
// This replaces the previous approach of building a sorted list + binary search.
func buildEnclosingByPos(file *ast.File, filePath string, fset *token.FileSet) map[token.Pos]string {
	enclosingByPos := make(map[token.Pos]string)

	// funcStack tracks the current enclosing function(s) during traversal
	var funcStack []string

	// Pre-allocate space for identifiers (typical Go file has many identifiers)
	// We'll collect all idents during traversal and assign enclosing funcs
	var inspector func(n ast.Node) bool
	inspector = func(n ast.Node) bool {
		if n == nil {
			return true
		}

		switch node := n.(type) {
		case *ast.FuncDecl:
			if node.Body == nil || node.Name == nil {
				return true
			}
			// Compute function ID
			namePos := fset.Position(node.Name.Pos())
			kind := KindFunc
			if node.Recv != nil {
				kind = KindMethod
			}
			funcID := generateID(filePath, namePos.Line, namePos.Column, string(kind))

			// Push onto stack, traverse body, pop
			funcStack = append(funcStack, funcID)
			ast.Inspect(node.Body, inspector)
			funcStack = funcStack[:len(funcStack)-1]

			return false // Already handled children

		case *ast.Ident:
			// Record enclosing function for this identifier
			if len(funcStack) > 0 {
				enclosingByPos[node.Pos()] = funcStack[len(funcStack)-1]
			}
		}

		return true
	}

	ast.Inspect(file, inspector)
	return enclosingByPos
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
