package store

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestOrderByClauses_EndInDeterministicTiebreaker scans all SQL embedded in
// non-test Go source and requires every ORDER BY clause to end in a plain
// column reference — not an aggregate, expression, or bare DESC key.
// Rationale (boot trap): ranking on non-unique keys without a tiebreaker
// makes golden tests flake; this enforces the convention at the source.
func TestOrderByClauses_EndInDeterministicTiebreaker(t *testing.T) {
	root := "../.."
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("repo root not found: %v", err)
	}

	var violations []string
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, v := range checkOrderByClauses(string(content)) {
				violations = append(violations, path+": ORDER BY "+v)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if len(violations) > 0 {
		t.Errorf("ORDER BY clauses whose final key is not a plain column (add a stable tiebreaker column):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

var orderByRe = regexp.MustCompile(`(?i)\bORDER BY\b`)

// inLineComment reports whether position i is preceded by "//" on its line.
func inLineComment(src string, i int) bool {
	lineStart := strings.LastIndexByte(src[:i], '\n') + 1
	return strings.Contains(src[lineStart:i], "//")
}

// checkOrderByClauses returns the offending clause text for every ORDER BY
// whose final sort key is not a plain column reference.
func checkOrderByClauses(src string) []string {
	var bad []string
	for _, loc := range orderByRe.FindAllStringIndex(src, -1) {
		if inLineComment(src, loc[0]) {
			continue
		}
		clause := extractClause(src[loc[1]:])
		keys := splitTopLevel(clause)
		if len(keys) == 0 {
			continue
		}
		last := strings.TrimSpace(keys[len(keys)-1])
		if !isPlainColumn(last) {
			bad = append(bad, strings.Join(strings.Fields(clause), " "))
		}
	}
	return bad
}

// extractClause reads from just after "ORDER BY" to the clause end: a
// backtick or quote ending the SQL literal, a top-level LIMIT/OFFSET, a
// semicolon, or a ')' that closes an enclosing paren (e.g. a window spec).
func extractClause(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '`', '"', ';':
			return s[:i]
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return s[:i]
			}
			depth--
		case 'L', 'l', 'O', 'o':
			if depth == 0 && (hasKeywordAt(s, i, "LIMIT") || hasKeywordAt(s, i, "OFFSET")) {
				return s[:i]
			}
		}
	}
	return s
}

func hasKeywordAt(s string, i int, kw string) bool {
	if i+len(kw) > len(s) || !strings.EqualFold(s[i:i+len(kw)], kw) {
		return false
	}
	before := i == 0 || isWordSep(s[i-1])
	after := i+len(kw) == len(s) || isWordSep(s[i+len(kw)])
	return before && after
}

func isWordSep(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// splitTopLevel splits a clause on commas outside parentheses.
func splitTopLevel(s string) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// isPlainColumn reports whether a sort key is a bare column reference,
// optionally suffixed ASC. DESC keys and expressions don't qualify: a
// descending ranking key is exactly the kind that ties.
func isPlainColumn(key string) bool {
	fields := strings.Fields(key)
	switch len(fields) {
	case 1:
		// fall through
	case 2:
		if !strings.EqualFold(fields[1], "ASC") {
			return false
		}
	default:
		return false
	}
	col := fields[0]
	if strings.ContainsAny(col, "()?+-*/") {
		return false
	}
	return regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`).MatchString(col)
}
