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

// ExtractCallGraph extracts the static call graph from loaded packages
func ExtractCallGraph(result *LoadResult, symbols []Symbol) ([]CallEdge, error) {
	// Build symbol lookup by definition position
	symbolByPos := buildSymbolPosIndex(symbols)

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

			// Build enclosing function map for this file
			enclosingMap := buildEnclosingMap(file, filePath, result.Fset)

			// Walk AST looking for call expressions
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				edge := extractCallEdge(pkg, call, filePath, result.Fset, symbolByPos, enclosingMap)
				if edge != nil {
					edges = append(edges, *edge)
				}

				return true
			})
		}
	}

	return edges, nil
}

func extractCallEdge(pkg *packages.Package, call *ast.CallExpr, filePath string, fset *token.FileSet, symbolByPos map[string]string, enclosingMap []enclosingFunc) *CallEdge {
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
	defKey := posKey(defPosInfo.Filename, defPosInfo.Line, defPosInfo.Column)

	// Look up the callee symbol ID
	calleeID, ok := symbolByPos[defKey]
	if !ok {
		return nil // Callee not in our index (e.g., stdlib function)
	}

	// Get call position
	callPos := fset.Position(call.Pos())

	// Find the enclosing function (caller)
	callerID := findEnclosing(call.Pos(), enclosingMap)
	if callerID == "" {
		return nil // Call not inside a function (e.g., init expression)
	}

	return &CallEdge{
		CallerID: callerID,
		CalleeID: calleeID,
		FilePath: filePath,
		Line:     callPos.Line,
		Col:      callPos.Column,
	}
}
