package index

import (
	"go/ast"
	"go/token"
	"go/types"

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
	return ExtractCallGraphFiltered(result, symbols, nil)
}

// ExtractCallGraphFiltered extracts the call graph, optionally limited to specific files.
// When onlyFiles is non-nil, only edges from those files are extracted.
func ExtractCallGraphFiltered(result *LoadResult, symbols []Symbol, onlyFiles map[string]bool) ([]CallEdge, error) {
	// Build symbol lookup by definition position (with fallback for chunked loading)
	symbolIndex := buildSymbolPosIndex(symbols)

	// Pre-compute method implementations across all loaded packages for
	// interface-dispatch resolution. Maps method name → candidates with
	// receiver types, so we can do a proper types.Implements check at each
	// interface call site (rather than the lossy "is the impl pkg directly
	// imported by the caller?" heuristic).
	methodImpls := buildMethodImplsByName(result, symbolIndex)

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

			// Skip files not in the filter set (if filtering)
			if onlyFiles != nil && !onlyFiles[filePath] {
				continue
			}

			// Extract call edges using AST walker with function stack tracking.
			// This avoids the separate buildEnclosingMap + findEnclosing passes.
			fileEdges := extractCallEdgesWithStack(pkg, file, filePath, result.Fset, symbolIndex, methodImpls)
			edges = append(edges, fileEdges...)
		}
	}

	return edges, nil
}

// methodImpl records a concrete method declaration: its symbol ID and its
// receiver type (preserving pointer-vs-value as declared).
type methodImpl struct {
	id       string
	recvType types.Type
}

// buildMethodImplsByName scans every loaded package's TypesInfo.Defs for
// method declarations and groups them by method name. The receiver type is
// retained so callers can run types.Implements against an interface type.
func buildMethodImplsByName(result *LoadResult, symbolIndex *SymbolPosIndex) map[string][]methodImpl {
	out := map[string][]methodImpl{}
	for _, pkg := range result.Packages {
		if pkg.TypesInfo == nil {
			continue
		}
		for ident, obj := range pkg.TypesInfo.Defs {
			fn, ok := obj.(*types.Func)
			if !ok || fn == nil {
				continue
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok || sig.Recv() == nil {
				continue
			}
			pos := result.Fset.Position(ident.Pos())
			id, ok := symbolIndex.Lookup(pos.Filename, pos.Line, pos.Column)
			if !ok {
				continue
			}
			out[fn.Name()] = append(out[fn.Name()], methodImpl{
				id:       id,
				recvType: sig.Recv().Type(),
			})
		}
	}
	return out
}

// implementsInterface reports whether recvT (or its pointer form) satisfies
// the interface. Tests both because pointer-receiver methods only satisfy the
// interface via *T, while value-receiver methods satisfy via either T or *T.
func implementsInterface(recvT types.Type, iface *types.Interface) bool {
	if iface == nil || iface.Empty() {
		return false
	}
	if types.Implements(recvT, iface) {
		return true
	}
	// If recvT is a value type, also check its pointer form.
	if _, isPtr := recvT.(*types.Pointer); !isPtr {
		if types.Implements(types.NewPointer(recvT), iface) {
			return true
		}
	}
	return false
}

// extractCallEdgesWithStack extracts all call edges from a file using a single AST pass.
// Maintains a function stack to track the current enclosing function for O(1) lookup.
func extractCallEdgesWithStack(pkg *packages.Package, file *ast.File, filePath string, fset *token.FileSet, symbolIndex *SymbolPosIndex, methodImpls map[string][]methodImpl) []CallEdge {
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
			// Extract call edges with current enclosing function
			newEdges := extractCallEdgeFromExpr(pkg, node, filePath, fset, symbolIndex, funcStack, methodImpls)
			edges = append(edges, newEdges...)
		}

		return true
	}

	ast.Inspect(file, inspector)
	return edges
}

// extractCallEdgeFromExpr extracts call edges from a call expression.
// Returns multiple edges when an interface method call resolves to concrete implementations.
// Uses the function stack for O(1) enclosing function lookup.
func extractCallEdgeFromExpr(pkg *packages.Package, call *ast.CallExpr, filePath string, fset *token.FileSet, symbolIndex *SymbolPosIndex, funcStack []string, methodImpls map[string][]methodImpl) []CallEdge {
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
	callPos := fset.Position(call.Pos())

	// Look up the callee symbol ID (with fallback for chunked loading)
	calleeID, ok := symbolIndex.Lookup(defPosInfo.Filename, defPosInfo.Line, defPosInfo.Column)
	if ok {
		return []CallEdge{{
			CallerID: callerID,
			CalleeID: calleeID,
			FilePath: filePath,
			Line:     callPos.Line,
			Col:      callPos.Column,
		}}
	}

	// Lookup failed — check if this is an interface method call.
	// When calling iface.Method(), TypesInfo.Uses resolves to the interface
	// definition, which has no symbol in our index. Fall back to matching
	// all concrete methods with the same name.
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	sig := fn.Type().(*types.Signature)
	recv := sig.Recv()
	if recv == nil {
		return nil
	}
	recvType := recv.Type()
	if ptr, ok := recvType.(*types.Pointer); ok {
		recvType = ptr.Elem()
	}
	named, ok := recvType.(*types.Named)
	if !ok {
		return nil
	}
	iface, isIface := named.Underlying().(*types.Interface)
	if !isIface {
		return nil
	}

	// Interface method call — create edges to every concrete method with the
	// same name whose receiver type satisfies the interface. types.Implements
	// is the correct semantic check; the previous "is the impl pkg directly
	// imported?" heuristic dropped legitimate cross-package dispatch in
	// dependency-inversion architectures (caller depends on interface, impl
	// lives in a separate package wired in elsewhere).
	candidates := methodImpls[fn.Name()]
	if len(candidates) == 0 {
		return nil
	}

	edges := make([]CallEdge, 0, len(candidates))
	for _, c := range candidates {
		if !implementsInterface(c.recvType, iface) {
			continue
		}
		edges = append(edges, CallEdge{
			CallerID: callerID,
			CalleeID: c.id,
			FilePath: filePath,
			Line:     callPos.Line,
			Col:      callPos.Column,
		})
	}
	return edges
}
