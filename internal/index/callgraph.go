package index

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/packages"
)

// CallEdge represents a call from one function to another
type CallEdge struct {
	CallerID string
	CalleeID string
	FilePath string
	Line     int
	Col      int
}

// ExtractCallGraph extracts the static call graph from loaded packages.
// Uses an optimized approach: single AST pass with function stack tracking
// for O(1) enclosing function lookup per call.
func ExtractCallGraph(result *LoadResult, symbols []Symbol) ([]CallEdge, error) {
	// Build symbol lookup by definition position (with fallback for chunked loading)
	symbolIndex := buildSymbolPosIndex(symbols)

	var edges []CallEdge

	for _, pkg := range result.Packages {
		if pkg.TypesInfo == nil {
			continue
		}

		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			filePath := pkg.GoFiles[i]

			// Extract call edges using AST walker with function stack tracking.
			// This avoids the separate buildEnclosingMap + findEnclosing passes.
			fileEdges := extractCallEdgesWithStack(pkg, file, filePath, result.Fset, symbolIndex)
			edges = append(edges, fileEdges...)
		}
	}

	return edges, nil
}

// extractCallEdgesWithStack extracts all call edges from a file using a single AST pass.
// Maintains a function stack to track the current enclosing function for O(1) lookup.
func extractCallEdgesWithStack(pkg *packages.Package, file *ast.File, filePath string, fset *token.FileSet, symbolIndex *SymbolPosIndex) []CallEdge {
	var edges []CallEdge
	var funcStack []string // Stack of enclosing function IDs

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

		case *ast.CallExpr:
			// Extract call edge with current enclosing function
			edge := extractCallEdgeFromExpr(pkg, node, filePath, fset, symbolIndex, funcStack)
			if edge != nil {
				edges = append(edges, *edge)
			}
		}

		return true
	}

	ast.Inspect(file, inspector)
	return edges
}

// extractCallEdgeFromExpr extracts a call edge from a call expression.
// Uses the function stack for O(1) enclosing function lookup.
func extractCallEdgeFromExpr(pkg *packages.Package, call *ast.CallExpr, filePath string, fset *token.FileSet, symbolIndex *SymbolPosIndex, funcStack []string) *CallEdge {
	// Get current enclosing function (caller)
	if len(funcStack) == 0 {
		return nil // Call not inside a function (e.g., init expression)
	}
	callerID := funcStack[len(funcStack)-1]

	// Get the callee identifier
	var calleeIdent *ast.Ident

	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// Direct function call: foo()
		calleeIdent = fn

	case *ast.SelectorExpr:
		// Method call or qualified call: x.Method() or pkg.Func()
		calleeIdent = fn.Sel

	default:
		// Function literal or other expression - can't track statically
		return nil
	}

	if calleeIdent == nil {
		return nil
	}

	// Look up the callee in TypesInfo.Uses
	obj := pkg.TypesInfo.Uses[calleeIdent]
	if obj == nil {
		return nil
	}

	// Get the definition position
	defPos := obj.Pos()
	if !defPos.IsValid() {
		return nil
	}

	defPosInfo := fset.Position(defPos)

	// Look up the callee symbol ID (with fallback for chunked loading)
	calleeID, ok := symbolIndex.Lookup(defPosInfo.Filename, defPosInfo.Line, defPosInfo.Column)
	if !ok {
		return nil // Callee not in our index (e.g., stdlib function)
	}

	// Get call position
	callPos := fset.Position(call.Pos())

	return &CallEdge{
		CallerID: callerID,
		CalleeID: calleeID,
		FilePath: filePath,
		Line:     callPos.Line,
		Col:      callPos.Column,
	}
}
