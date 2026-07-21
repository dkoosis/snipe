package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/dkoosis/snipe/internal/util"
)

// StringRef is a reference to a string literal or env var name in source.
type StringRef struct {
	ID          string // 16-char hex
	Value       string // the string value, e.g. "SNIPE_VOYAGE_API_KEY"
	Name        string // for const refs: the declared identifier name
	Kind        string // "env", "const", or "fixture"
	FilePath    string
	Line        int
	Col         int
	EnclosingID string // enclosing symbol ID (nullable)
	Snippet     string // source line for context
}

// ExtractLiterals extracts string literals from all packages in the result.
func ExtractLiterals(result *LoadResult, symbols []Symbol) []StringRef {
	return ExtractLiteralsFiltered(result, symbols, nil)
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

// extractFileLiterals walks a single file's AST extracting string literals in
// three shapes: os.Getenv-family args (kind=env), named string consts
// (kind=const), and inline fixture-path literals (kind=fixture).
//
// The fixture pass (sn-8f6q.9) is the bounded broadening that lets `snipe plan`'s
// churn detector fire on inline golden-path arguments — e.g.
// golden.Assert(t, got, "testdata/x.golden") or os.ReadFile("testdata/foo.golden")
// — which the env/const passes miss. The gate is a predicate on the VALUE:
// contains "testdata/" or ends in ".golden" (see fixtureLikePath). That is
// deliberately the SAME predicate FindChurnLiterals queries, so extraction and
// query stay aligned — we index exactly what churn can consume, nothing more.
//
// Why this doesn't balloon the index: only fixture-shaped literals qualify. An
// ordinary inline string (a log message, a SQL fragment, an arbitrary path)
// never matches, so a normal repo contributes only its handful of real fixture
// paths. Indexing every inline literal instead would multiply string_refs by
// orders of magnitude for zero churn benefit — the predicate is the bound. (An
// allowlist of golden-helper function names was the alternative gate, but it
// would drift from churn's value predicate and miss os.ReadFile-style reads.)
//
// Dedup: ast.Inspect is pre-order, so a CallExpr/GenDecl is visited before its
// child BasicLit. The env/const passes record each literal position they consume
// in `claimed`; the fixture pass skips any BasicLit already claimed. A const
// whose value happens to match the fixture predicate therefore stays a single
// kind=const row — churn still finds it (churn keys on value, not kind).
func extractFileLiterals(file *ast.File, filePath string, fset *token.FileSet) []StringRef {
	var refs []StringRef
	claimed := make(map[token.Pos]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			refs = append(refs, extractEnvCall(node, filePath, fset, claimed)...)
		case *ast.GenDecl:
			refs = append(refs, extractConstLiterals(node, filePath, fset, claimed)...)
		case *ast.BasicLit:
			if r, ok := extractFixtureLiteral(node, filePath, fset, claimed); ok {
				refs = append(refs, r)
			}
		}
		return true
	})
	return refs
}

// extractEnvCall extracts os.Getenv / os.LookupEnv / os.Setenv calls.
func extractEnvCall(call *ast.CallExpr, filePath string, fset *token.FileSet, claimed map[token.Pos]bool) []StringRef {
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
	claimed[lit.Pos()] = true
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
func extractConstLiterals(decl *ast.GenDecl, filePath string, fset *token.FileSet, claimed map[token.Pos]bool) []StringRef {
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
			claimed[lit.Pos()] = true
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

// fixtureLikePath reports whether a string value looks like a test fixture path.
// This is the exact gate FindChurnLiterals queries (LIKE '%testdata/%' OR
// '%.golden'); keeping the two in sync means the extractor indexes precisely the
// literals churn can surface.
func fixtureLikePath(value string) bool {
	return strings.Contains(value, "testdata/") || strings.HasSuffix(value, ".golden")
}

// extractFixtureLiteral indexes an inline string literal that looks like a
// fixture path, unless it was already claimed by the env/const passes. See
// extractFileLiterals for the gate rationale.
func extractFixtureLiteral(lit *ast.BasicLit, filePath string, fset *token.FileSet, claimed map[token.Pos]bool) (StringRef, bool) {
	if lit.Kind != token.STRING || claimed[lit.Pos()] {
		return StringRef{}, false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || !fixtureLikePath(value) {
		return StringRef{}, false
	}
	claimed[lit.Pos()] = true
	pos := fset.Position(lit.Pos())
	return StringRef{
		Value:    value,
		Kind:     "fixture",
		FilePath: filePath,
		Line:     pos.Line,
		Col:      pos.Column,
	}, true
}

// literalID generates a stable 16-char hex ID for a string ref.
func literalID(r StringRef) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%d:%s", r.FilePath, r.Line, r.Col, r.Value))
	return hex.EncodeToString(h[:8])
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
