package index

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
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
	ASTCtx      string // Syntactic context of the ref (see CtxKind values)
}

// AST context kinds stored on each ref. These are syntactic facts recorded
// at index time so downstream consumers (lifecycle classification) never
// have to re-derive them from snippet text.
const (
	CtxCompositeLit = "lit"      // type position of a composite literal: T{...}, &T{...}
	CtxNew          = "new"      // argument of new(T)
	CtxMake         = "make"     // argument of make([]T, ...)
	CtxSignature    = "sig"      // within a func declaration/literal signature
	CtxTypeDecl     = "typedecl" // within a type declaration (struct field, interface method)
	CtxCallPrefix   = "call:"    // argument of a call; suffix is the callee name
)

// ExtractRefs extracts all references from loaded packages.
// For better performance during indexing, use ExtractRefsWithCache.
func ExtractRefs(result *LoadResult, symbols []Symbol) ([]Ref, error) {
	return ExtractRefsFiltered(result, symbols, nil, nil)
}

// ExtractRefsWithCache extracts all references from loaded packages.
// If cache is provided, file contents are cached to avoid repeated disk reads.
// The cache should be cleared after indexing completes to free memory.
func ExtractRefsWithCache(result *LoadResult, symbols []Symbol, cache *util.FileCache) ([]Ref, error) {
	return ExtractRefsFiltered(result, symbols, cache, nil)
}

// ExtractRefsFiltered extracts references, optionally limited to specific files.
// When onlyFiles is non-nil, only refs in those files are extracted.
// Symbols are still indexed from all files for cross-file ref resolution.
func ExtractRefsFiltered(result *LoadResult, symbols []Symbol, cache *util.FileCache, onlyFiles map[string]bool) ([]Ref, error) {
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

			// Skip files not in the filter set (if filtering)
			if onlyFiles != nil && !onlyFiles[filePath] {
				continue
			}

			// Load file content for snippets (with optional caching)
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

			// Build enclosing function map for this file
			enclosingMap := buildEnclosingMap(file, filePath, result.Fset)

			// Build syntactic context ranges for this file
			ctxRanges := buildCtxRanges(file)

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

				// Look up the symbol ID (with fallback for chunked loading)
				symbolID, ok := symbolByPos.Lookup(defPosInfo.Filename, defPosInfo.Line, defPosInfo.Column)
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
					ASTCtx:      findCtx(ident.Pos(), ctxRanges),
				}
				refs = append(refs, ref)
			}
		}
	}

	return refs, nil
}

// SymbolPosIndex provides position-based symbol lookup with fallback for chunked loading.
// When packages are loaded in chunks, obj.Pos() may return declaration start (col 1)
// instead of identifier position. The fallback index handles this case.
type SymbolPosIndex struct {
	exact    map[string]string // file:line:col -> symbol ID
	fallback map[string]string // file:line -> symbol ID (for col 1 lookups)
}

// buildSymbolPosIndex creates a position index with exact and fallback lookups.
func buildSymbolPosIndex(symbols []Symbol) *SymbolPosIndex {
	idx := &SymbolPosIndex{
		exact:    make(map[string]string),
		fallback: make(map[string]string),
	}
	for i := range symbols {
		sym := &symbols[i]
		// Exact key using identifier position
		exactKey := posKey(sym.FilePath, sym.NameLine, sym.NameCol)
		idx.exact[exactKey] = sym.ID

		// Fallback key for declaration start (col 1) - only for funcs/methods
		// which are the primary callees we care about
		if sym.Kind == KindFunc || sym.Kind == KindMethod {
			fallbackKey := posKey(sym.FilePath, sym.NameLine, 1)
			// Don't overwrite if multiple symbols on same line
			if _, exists := idx.fallback[fallbackKey]; !exists {
				idx.fallback[fallbackKey] = sym.ID
			}
		}
	}
	return idx
}

// Lookup finds a symbol ID by position, trying exact match first then fallback.
func (idx *SymbolPosIndex) Lookup(file string, line, col int) (string, bool) {
	key := posKey(file, line, col)
	if id, ok := idx.exact[key]; ok {
		return id, true
	}
	// Fallback for chunked loading where col may be 1 (declaration start).
	// When col == 1, key is already "file:line:1" so reuse it.
	if col == 1 {
		if id, ok := idx.fallback[key]; ok {
			return id, true
		}
	}
	return "", false
}

func posKey(file string, line, col int) string {
	return file + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}

// ctxRange is a source range carrying the syntactic context of identifiers
// inside it (composite literal type, call arguments, signature, type decl).
type ctxRange struct {
	start, end token.Pos
	kind       string
}

// buildCtxRanges collects syntactic context ranges for a file in one pass.
// Ranges nest (a literal inside a call); findCtx resolves to the innermost.
func buildCtxRanges(file *ast.File) []ctxRange {
	var ranges []ctxRange
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			// Only the type position: in T{F: x} a ref to T is creation
			// evidence, a ref to x inside the braces is not.
			if node.Type != nil {
				ranges = append(ranges, ctxRange{node.Type.Pos(), node.Type.End(), CtxCompositeLit})
			}
		case *ast.CallExpr:
			switch fun := node.Fun.(type) {
			case *ast.Ident:
				switch fun.Name {
				case "new":
					ranges = append(ranges, ctxRange{node.Lparen, node.Rparen, CtxNew})
				case "make":
					ranges = append(ranges, ctxRange{node.Lparen, node.Rparen, CtxMake})
				default:
					ranges = append(ranges, ctxRange{node.Lparen, node.Rparen, CtxCallPrefix + fun.Name})
				}
			case *ast.SelectorExpr:
				ranges = append(ranges, ctxRange{node.Lparen, node.Rparen, CtxCallPrefix + fun.Sel.Name})
			}
		case *ast.FuncDecl:
			start := node.Type.Pos()
			if node.Recv != nil {
				start = node.Recv.Pos()
			}
			ranges = append(ranges, ctxRange{start, node.Type.End(), CtxSignature})
		case *ast.FuncLit:
			ranges = append(ranges, ctxRange{node.Type.Pos(), node.Type.End(), CtxSignature})
		case *ast.TypeSpec:
			ranges = append(ranges, ctxRange{node.Pos(), node.End(), CtxTypeDecl})
		}
		return true
	})

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start < ranges[j].start
	})
	return ranges
}

// findCtx returns the innermost context containing pos, or "". Because
// nested ranges start later than their parents, the first containing range
// found walking backward from the insertion point is the innermost.
func findCtx(pos token.Pos, ranges []ctxRange) string {
	idx := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].start > pos
	})
	for i := idx - 1; i >= 0; i-- {
		if pos >= ranges[i].start && pos < ranges[i].end {
			return ranges[i].kind
		}
	}
	return ""
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
			// Start at fn.Pos() (the func keyword), not the body Lbrace, so
			// refs in the signature (params, return types) are attributed to
			// the function rather than left with no enclosing symbol.
			funcs = append(funcs, enclosingFunc{
				id:    generateID(filePath, namePos.Line, namePos.Column, string(kind)),
				start: fn.Pos(),
				end:   fn.Body.Rbrace,
			})
		}
		return true
	})

	// Sort by start position for binary search
	sort.Slice(funcs, func(i, j int) bool {
		return funcs[i].start < funcs[j].start
	})

	return funcs
}

// findEnclosing uses binary search to find the enclosing function for a position.
// O(log N) instead of O(N) linear scan.
func findEnclosing(pos token.Pos, funcs []enclosingFunc) string {
	if len(funcs) == 0 {
		return ""
	}

	// Binary search for rightmost function where start <= pos
	idx := sort.Search(len(funcs), func(i int) bool {
		return funcs[i].start > pos
	})

	// Check functions from idx-1 down to 0 (nested functions possible)
	for i := idx - 1; i >= 0; i-- {
		if pos >= funcs[i].start && pos <= funcs[i].end {
			return funcs[i].id
		}
		// If this function ends before pos, earlier functions won't contain pos either
		// (unless they're nested, but Go doesn't have nested named functions)
		if funcs[i].end < pos {
			break
		}
	}

	return ""
}
