package context

import (
	"regexp"
	"strings"
)

// Column is a parsed SQL column: name and declared type. Constraints
// (PRIMARY KEY, NOT NULL, DEFAULT, ...) are intentionally dropped — a future
// "table X: columns a, b, c" render needs name+type, not full fidelity.
type Column struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
}

// Table is a parsed CREATE TABLE statement: its name and column list.
type Table struct {
	Name    string   `json:"name" yaml:"name"`
	Columns []Column `json:"columns" yaml:"columns"`
}

// createTableRe finds each CREATE TABLE statement's start; everything after
// the match is the table name followed by its column-list parens.
var createTableRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?`)

// tableConstraintRe matches a column-list entry that is a table-level
// constraint rather than a column definition: PRIMARY KEY(...), FOREIGN
// KEY(...), UNIQUE(...), CHECK(...), or a named CONSTRAINT.
var tableConstraintRe = regexp.MustCompile(`(?i)^(PRIMARY\s+KEY|FOREIGN\s+KEY|UNIQUE\s*\(|CHECK\s*\(|CONSTRAINT\b)`)

// ParseSchema parses one or more CREATE TABLE statements out of ddl (as
// produced by DetectDBSchemas) into a table -> columns structure. It is a
// lightweight tokenizer over the common SQL subset (SQLite/Postgres/MySQL
// CREATE TABLE), not a full SQL-dialect parser: CREATE INDEX/VIEW/TRIGGER
// statements are ignored, and column types are captured verbatim up to the
// first constraint keyword (name + type is enough for the intended render).
func ParseSchema(ddl string) []Table {
	var tables []Table
	locs := createTableRe.FindAllStringIndex(ddl, -1)
	for _, loc := range locs {
		rest := ddl[loc[1]:]
		name, afterName := readIdent(rest)
		if name == "" {
			continue
		}
		openIdx := strings.IndexByte(afterName, '(')
		if openIdx < 0 {
			continue
		}
		body, ok := matchParens(afterName[openIdx:])
		if !ok {
			continue
		}
		tables = append(tables, Table{
			Name:    name,
			Columns: parseColumns(body),
		})
	}
	return tables
}

// parseColumns splits a CREATE TABLE column-list body on top-level commas
// and extracts a name+type pair from each entry that is a column definition
// (as opposed to a table-level constraint).
func parseColumns(body string) []Column {
	var cols []Column
	for _, part := range splitTopLevel(body, ',') {
		part = strings.TrimSpace(part)
		if part == "" || tableConstraintRe.MatchString(part) {
			continue
		}
		name, rest := readIdent(part)
		if name == "" {
			continue
		}
		typ := readType(rest)
		if typ == "" {
			continue
		}
		cols = append(cols, Column{Name: name, Type: typ})
	}
	return cols
}

// readIdent reads one identifier from the front of s, skipping leading
// whitespace. Handles quoted identifiers (backtick, double-quote, bracket)
// as well as bare word characters. Returns the identifier (unquoted) and
// whatever remains of s after it.
func readIdent(s string) (ident string, rest string) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	if i >= len(s) {
		return "", s
	}
	switch s[i] {
	case '`':
		if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
			return s[i+1 : i+1+end], s[i+1+end+1:]
		}
		return "", s
	case '"':
		if end := strings.IndexByte(s[i+1:], '"'); end >= 0 {
			return s[i+1 : i+1+end], s[i+1+end+1:]
		}
		return "", s
	case '[':
		if end := strings.IndexByte(s[i+1:], ']'); end >= 0 {
			return s[i+1 : i+1+end], s[i+1+end+1:]
		}
		return "", s
	}
	j := i
	for j < len(s) && isSchemaIdentByte(s[j]) {
		j++
	}
	if j == i {
		return "", s
	}
	return s[i:j], s[j:]
}

// isSchemaIdentByte reports whether c can appear in a bare SQL identifier as
// read by readIdent. Includes '.' for schema-qualified table names
// (schema.table), which the repo's general-purpose isIdentByte (roles.go)
// doesn't need.
func isSchemaIdentByte(c byte) bool {
	return c == '_' || c == '.' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// readType reads a column's declared type from the front of s: the next
// word, plus a following parenthesized precision/scale if present (e.g.
// "VARCHAR(255)", "DECIMAL(10,2)"). Anything after that — NOT NULL, DEFAULT,
// PRIMARY KEY, REFERENCES, etc. — is a constraint, not part of the type, and
// is dropped.
func readType(s string) string {
	name, rest := readIdent(s)
	if name == "" {
		return ""
	}
	rest = strings.TrimLeft(rest, " \t\n\r")
	if strings.HasPrefix(rest, "(") {
		if inner, ok := matchParens(rest); ok {
			return name + "(" + strings.TrimSpace(inner) + ")"
		}
	}
	return name
}

// matchParens expects s to start with '(' and returns the content between it
// and its matching close paren, honoring nested parens and single-quoted
// string literals. Callers only need the inner content — none track where
// the match ends — so unlike the exported matchers elsewhere in this
// package, it has no "rest" return.
func matchParens(s string) (inner string, ok bool) {
	if len(s) == 0 || s[0] != '(' {
		return "", false
	}
	depth := 0
	i := 0
	for i < len(s) {
		switch s[i] {
		case '\'':
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			i = j
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
		i++
	}
	return "", false
}

// splitTopLevel splits s on sep, ignoring occurrences inside nested parens or
// single-quoted string literals.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	start := 0
	i := 0
	for i < len(s) {
		switch s[i] {
		case '\'':
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			i = j
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
		i++
	}
	parts = append(parts, s[start:])
	return parts
}
