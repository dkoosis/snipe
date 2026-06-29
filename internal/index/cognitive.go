package index

import (
	"go/ast"
	"go/token"
)

// computeCognitive returns the SonarSource cognitive complexity of a function
// body (Campbell, "Cognitive Complexity: A new way of measuring
// understandability", SonarSource 2017).
//
// Where McCabe's cyclomatic complexity counts independent paths, cognitive
// complexity estimates how hard code is to *understand*: it penalizes nesting
// and counts a switch once (not once per case), so a flat dispatch scores low
// while deeply nested control flow scores high. It complements, not replaces,
// cyclo. Body=nil (interface methods, external decls) → 0.
//
// Three rules from the white paper:
//
//	B1 — +1 for each break in linear flow: if / else-if / else, switch,
//	     for / range, select, labeled break|continue|goto, and each sequence
//	     of like binary boolean operators (&& / ||).
//	B2 — +1 extra for each level of nesting around a B1 control structure.
//	B3 — no increment for shorthand: the bare `else`/`else if` take only the
//	     flat B1 increment (no nesting penalty), and a nested func literal adds
//	     a nesting level but no increment of its own.
func computeCognitive(body *ast.BlockStmt) int {
	if body == nil {
		return 0
	}
	c := &cognitiveCounter{}
	c.walkStmts(body.List, 0)
	return c.total
}

type cognitiveCounter struct{ total int }

func (c *cognitiveCounter) walkStmts(stmts []ast.Stmt, nesting int) {
	for _, s := range stmts {
		c.walkStmt(s, nesting)
	}
}

func (c *cognitiveCounter) walkStmt(stmt ast.Stmt, nesting int) {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		c.total += 1 + nesting // B1 + B2
		if s.Init != nil {
			c.walkStmt(s.Init, nesting)
		}
		c.walkExpr(s.Cond, nesting)
		c.walkStmts(s.Body.List, nesting+1)
		c.walkElse(s.Else, nesting)
	case *ast.ForStmt:
		c.total += 1 + nesting
		if s.Cond != nil {
			c.walkExpr(s.Cond, nesting)
		}
		c.walkStmts(s.Body.List, nesting+1)
	case *ast.RangeStmt:
		c.total += 1 + nesting
		c.walkStmts(s.Body.List, nesting+1)
	case *ast.SwitchStmt:
		c.total += 1 + nesting // counted once, regardless of case count
		if s.Tag != nil {
			c.walkExpr(s.Tag, nesting)
		}
		c.walkStmts(s.Body.List, nesting+1)
	case *ast.TypeSwitchStmt:
		c.total += 1 + nesting
		c.walkStmts(s.Body.List, nesting+1)
	case *ast.SelectStmt:
		c.total += 1 + nesting
		c.walkStmts(s.Body.List, nesting+1)
	case *ast.CaseClause:
		for _, e := range s.List {
			c.walkExpr(e, nesting)
		}
		c.walkStmts(s.Body, nesting) // case body stays at the switch's nesting
	case *ast.CommClause:
		c.walkStmts(s.Body, nesting)
	case *ast.BranchStmt:
		if s.Label != nil { // labeled break / continue / goto break linear flow
			c.total++
		}
	case *ast.LabeledStmt:
		c.walkStmt(s.Stmt, nesting)
	case *ast.BlockStmt:
		c.walkStmts(s.List, nesting)
	case *ast.ExprStmt:
		c.walkExpr(s.X, nesting)
	case *ast.AssignStmt:
		for _, e := range s.Rhs {
			c.walkExpr(e, nesting)
		}
	case *ast.ReturnStmt:
		for _, e := range s.Results {
			c.walkExpr(e, nesting)
		}
	case *ast.GoStmt:
		c.walkExpr(s.Call, nesting)
	case *ast.DeferStmt:
		c.walkExpr(s.Call, nesting)
	}
}

// walkElse handles the else chain. Both `else if` and a bare `else` take a
// single flat increment with no nesting penalty (rule B3); their bodies sit
// at the same depth as the matching if-block.
func (c *cognitiveCounter) walkElse(e ast.Stmt, nesting int) {
	switch s := e.(type) {
	case nil:
		return
	case *ast.IfStmt: // else if
		c.total++
		c.walkExpr(s.Cond, nesting)
		c.walkStmts(s.Body.List, nesting+1)
		c.walkElse(s.Else, nesting)
	case *ast.BlockStmt: // bare else
		c.total++
		c.walkStmts(s.List, nesting+1)
	}
}

func (c *cognitiveCounter) walkExpr(expr ast.Expr, nesting int) {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			c.countLogical(e, token.ILLEGAL, nesting)
			return
		}
		c.walkExpr(e.X, nesting)
		c.walkExpr(e.Y, nesting)
	case *ast.ParenExpr:
		c.walkExpr(e.X, nesting)
	case *ast.UnaryExpr:
		c.walkExpr(e.X, nesting)
	case *ast.CallExpr:
		c.walkExpr(e.Fun, nesting)
		for _, a := range e.Args {
			c.walkExpr(a, nesting)
		}
	case *ast.FuncLit: // nested function: a nesting level, no flat increment
		if e.Body != nil {
			c.walkStmts(e.Body.List, nesting+1)
		}
	}
}

// countLogical adds +1 for each maximal run of the same boolean operator.
// `a && b && c` is one sequence (+1); `a && b || c` alternates (+2).
func (c *cognitiveCounter) countLogical(expr ast.Expr, parentOp token.Token, nesting int) {
	be, ok := expr.(*ast.BinaryExpr)
	if !ok || (be.Op != token.LAND && be.Op != token.LOR) {
		c.walkExpr(expr, nesting) // operand may hold a func literal, etc.
		return
	}
	if be.Op != parentOp {
		c.total++
	}
	c.countLogical(be.X, be.Op, nesting)
	c.countLogical(be.Y, be.Op, nesting)
}
