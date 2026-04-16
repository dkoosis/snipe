package context

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DBSchema is a detected SQLite schema definition from a static source in the repo.
type DBSchema struct {
	// Source is the repo-relative path the DDL was read from.
	Source string `json:"source" yaml:"source"`
	// Name is a short label for the schema (package name, "migrations", or "schema").
	Name string `json:"name" yaml:"name"`
	// DDL is the compacted CREATE TABLE / INDEX / VIEW statements.
	DDL string `json:"ddl" yaml:"ddl"`
}

// maxDDLBytes caps per-schema DDL output to keep the context token budget sane.
// Large schemas truncate with a pointer to the source.
const maxDDLBytes = 6000

// skipDirs are walk directories that never contain authoritative DDL.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
	"testdata":     true,
	".sandbox":     true,
	".quality":     true,
	".snipe":       true,
	".cache":       true,
	"dist":         true,
	"bin":          true,
}

// ddlRe matches CREATE TABLE|INDEX|VIEW|TRIGGER statements (case-insensitive).
// Used to decide whether a string literal or .sql file contains DDL worth emitting.
var ddlRe = regexp.MustCompile(`(?i)\bCREATE\s+(TABLE|INDEX|UNIQUE\s+INDEX|VIEW|TRIGGER)\b`)

// DetectDBSchemas returns all SQLite schemas statically detectable from the repo.
// Sources, in precedence order for any given root:
//  1. migrations/*.sql or db/migrations/*.sql directories (coalesced)
//  2. schema.sql, db/schema.sql, sql/schema.sql at repo root
//  3. Embedded CREATE TABLE|INDEX|VIEW literals in Go source files
//
// Migrations at the root suppress a top-level schema.sql at the same root (they
// typically document the same database). Embedded Go DDL is always emitted —
// it commonly represents independent databases in separate packages.
func DetectDBSchemas(repoRoot string) []DBSchema {
	var out []DBSchema

	migrationsFound := false
	if s, ok := detectMigrations(repoRoot); ok {
		out = append(out, s)
		migrationsFound = true
	}

	if !migrationsFound {
		if s, ok := detectTopLevelSQL(repoRoot); ok {
			out = append(out, s)
		}
	}

	out = append(out, detectEmbeddedGo(repoRoot)...)

	return out
}

// detectMigrations coalesces .sql files from a migrations/ or db/migrations/ directory.
func detectMigrations(repoRoot string) (DBSchema, bool) {
	for _, candidate := range []string{"migrations", "db/migrations", "sql/migrations"} {
		dir := filepath.Join(repoRoot, candidate)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
		if len(files) == 0 {
			continue
		}
		sort.Strings(files)
		var buf strings.Builder
		for _, f := range files {
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString("-- " + filepath.Base(f) + "\n")
			buf.Write(content)
		}
		return DBSchema{
			Source: relPath(repoRoot, dir),
			Name:   "migrations",
			DDL:    compactDDL(buf.String()),
		}, true
	}
	return DBSchema{}, false
}

// detectTopLevelSQL looks for a standalone schema.sql file.
func detectTopLevelSQL(repoRoot string) (DBSchema, bool) {
	for _, candidate := range []string{"schema.sql", "db/schema.sql", "sql/schema.sql"} {
		path := filepath.Join(repoRoot, candidate)
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !ddlRe.MatchString(string(content)) {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(candidate), ".sql")
		return DBSchema{
			Source: relPath(repoRoot, path),
			Name:   name,
			DDL:    compactDDL(string(content)),
		}, true
	}
	return DBSchema{}, false
}

// detectEmbeddedGo walks .go files and extracts DDL from backtick string literals.
// Each file containing DDL produces one DBSchema, named after its parent directory.
func detectEmbeddedGo(repoRoot string) []DBSchema {
	var out []DBSchema
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		ddl := extractGoDDL(string(content))
		if ddl == "" {
			return nil
		}
		name := filepath.Base(filepath.Dir(path))
		out = append(out, DBSchema{
			Source: relPath(repoRoot, path),
			Name:   name,
			DDL:    compactDDL(ddl),
		})
		return nil
	})
	// Stable order so multi-schema output is deterministic.
	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}

// extractGoDDL pulls the contents of every backtick raw string literal in src
// that contains a CREATE statement. Returns concatenated DDL, or "" if none.
func extractGoDDL(src string) string {
	var parts []string
	remaining := src
	for {
		start := strings.IndexByte(remaining, '`')
		if start < 0 {
			break
		}
		rest := remaining[start+1:]
		end := strings.IndexByte(rest, '`')
		if end < 0 {
			break
		}
		literal := rest[:end]
		if ddlRe.MatchString(literal) {
			parts = append(parts, dedentString(literal))
		}
		remaining = rest[end+1:]
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// dedentString applies dedentLines to a single string blob.
func dedentString(s string) string {
	return strings.Join(dedentLines(strings.Split(s, "\n")), "\n")
}

// compactDDL normalizes whitespace, strips comments, and truncates if huge.
func compactDDL(ddl string) string {
	lines := strings.Split(ddl, "\n")
	var kept []string
	prevBlank := true
	for _, ln := range lines {
		// Strip -- line comments but preserve content before them.
		if idx := strings.Index(ln, "--"); idx >= 0 {
			// Keep only our own file-header marker comments (-- filename.sql).
			if idx == 0 {
				kept = append(kept, ln)
				prevBlank = false
				continue
			}
			ln = ln[:idx]
		}
		trimmed := strings.TrimRight(ln, " \t")
		if strings.TrimSpace(trimmed) == "" {
			if prevBlank {
				continue
			}
			kept = append(kept, "")
			prevBlank = true
			continue
		}
		kept = append(kept, trimmed)
		prevBlank = false
	}
	kept = dedentLines(kept)
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if len(out) > maxDDLBytes {
		out = out[:maxDDLBytes] + "\n-- ... (truncated; see source file for full DDL)"
	}
	return out
}

// dedentLines strips the longest common leading-whitespace prefix from all
// non-blank lines. Embedded DDL from Go string literals often carries the
// source file's indentation; removing it cuts tokens and improves readability.
func dedentLines(lines []string) []string {
	common := ""
	first := true
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		prefix := leadingWhitespace(ln)
		if first {
			common = prefix
			first = false
			continue
		}
		common = commonPrefix(common, prefix)
		if common == "" {
			return lines
		}
	}
	if common == "" {
		return lines
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strings.TrimPrefix(ln, common)
	}
	return out
}

func leadingWhitespace(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return s
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

// relPath returns path relative to repoRoot using forward slashes, falling back
// to the absolute path if relative resolution fails.
func relPath(repoRoot, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
