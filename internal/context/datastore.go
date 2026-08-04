package context

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DataStore is a detected directory-of-files data store: Go code that writes
// structured documents (markdown, JSON, ...) into a directory, one file per
// logical record, as opposed to a SQL database (DBSchema) or a single
// fixed-name state file. It's a sibling to DBSchema rather than a unified
// type — the shapes (DDL text vs. a directory + naming convention) don't
// unify cleanly.
type DataStore struct {
	// Source is the repo-relative .go file the pattern was detected in.
	Source string `json:"source" yaml:"source"`
	// Dir is the expression that builds the directory path (e.g.
	// `filepath.Join(root, "docs", "diagrams")`), for a human/Claude reading
	// the detection rather than the raw source.
	Dir string `json:"dir" yaml:"dir"`
	// Pattern is the expression that builds each file's name inside Dir
	// (e.g. `name+".md"`). A computed (non-literal) pattern is exactly the
	// signal that Dir holds many differently-named files, not one fixed one.
	Pattern string `json:"pattern" yaml:"pattern"`
}

// mkdirAllVarRe captures the directory variable passed to os.MkdirAll.
var mkdirAllVarRe = regexp.MustCompile(`os\.MkdirAll\(\s*([A-Za-z_]\w*)\s*,`)

// dirJoinAssignRe captures how a MkdirAll'd variable's directory path was
// built, when it's a simple `dir := filepath.Join(...)` assignment — used
// only to render a readable Dir field, not for detection itself.
var dirJoinAssignRe = regexp.MustCompile(`([A-Za-z_]\w*)\s*:?=\s*filepath\.Join\(([^)]*)\)`)

// pathJoinAssignRe captures `pathVar := filepath.Join(dirVar, filenameExpr)`
// — the per-file path built from a MkdirAll'd directory plus a filename
// expression.
var pathJoinAssignRe = regexp.MustCompile(`(\w+)\s*:?=\s*filepath\.Join\(\s*(\w+)\s*,\s*([^,)]+)\)`)

// writeFileVarRe captures the (bare identifier) path argument passed to
// os.WriteFile or atomicfile.WriteFile — both are whole-file durable writes,
// so a store written atomically (github.com/dkoosis/atomicfile) is the same
// directory-of-documents signal as one written with os.WriteFile.
var writeFileVarRe = regexp.MustCompile(`(?:os|atomicfile)\.WriteFile\(\s*(\w+)\s*,`)

// writeFileInlineJoinRe captures the inline form
// os.WriteFile(filepath.Join(dirVar, filenameExpr), ...) — or the atomicfile
// equivalent — where the path is built directly in the call rather than via
// an intermediate variable.
var writeFileInlineJoinRe = regexp.MustCompile(`(?:os|atomicfile)\.WriteFile\(\s*filepath\.Join\(\s*(\w+)\s*,\s*([^,)]+)\)`)

// DetectDataStores scans .go files under repoRoot for the MkdirAll +
// WriteFile idiom that signals a directory-of-documents store: a directory
// created with os.MkdirAll, then written into via os.WriteFile (or
// atomicfile.WriteFile) at a path whose filename component is computed (a
// variable, concatenation, etc.)
// rather than a fixed string literal. A fixed literal filename (e.g.
// "session.json") means the directory holds one file, not a collection —
// that's a state file, not a data store, and is intentionally excluded.
//
// This is a static-text heuristic, not a data-flow analysis: it looks for
// the idiom's textual shape within a single file rather than tracing
// variables through calls or across files. It's proportionate to what a
// "surface this as a candidate store" detector needs, not a guarantee.
func DetectDataStores(repoRoot string) []DataStore {
	var out []DataStore
	_ = filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
			return err
		}
		rel := relPath(repoRoot, path)
		out = append(out, detectDataStoresInSource(rel, string(content))...)
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Dir < out[j].Dir
	})
	return out
}

// detectDataStoresInSource applies the MkdirAll+WriteFile idiom to one
// file's source text.
func detectDataStoresInSource(source, src string) []DataStore {
	mkdirVars := map[string]bool{}
	for _, m := range mkdirAllVarRe.FindAllStringSubmatch(src, -1) {
		mkdirVars[m[1]] = true
	}
	if len(mkdirVars) == 0 {
		return nil
	}

	writeFileVars := map[string]bool{}
	for _, m := range writeFileVarRe.FindAllStringSubmatch(src, -1) {
		writeFileVars[m[1]] = true
	}

	var out []DataStore
	seen := map[string]bool{}

	// Indirect form: pathVar := filepath.Join(dirVar, filenameExpr), later
	// passed to os.WriteFile(pathVar, ...).
	for _, m := range pathJoinAssignRe.FindAllStringSubmatch(src, -1) {
		pathVar, dirVar, filenameExpr := m[1], m[2], strings.TrimSpace(m[3])
		if !mkdirVars[dirVar] || !writeFileVars[pathVar] || isQuotedLiteral(filenameExpr) {
			continue
		}
		key := dirVar + "|" + filenameExpr
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, DataStore{
			Source:  source,
			Dir:     dirExprFor(src, dirVar),
			Pattern: filenameExpr,
		})
	}

	// Inline form: os.WriteFile(filepath.Join(dirVar, filenameExpr), ...).
	for _, m := range writeFileInlineJoinRe.FindAllStringSubmatch(src, -1) {
		dirVar, filenameExpr := m[1], strings.TrimSpace(m[2])
		if !mkdirVars[dirVar] || isQuotedLiteral(filenameExpr) {
			continue
		}
		key := dirVar + "|" + filenameExpr
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, DataStore{
			Source:  source,
			Dir:     dirExprFor(src, dirVar),
			Pattern: filenameExpr,
		})
	}

	return out
}

// isQuotedLiteral reports whether expr is nothing but a quoted string
// literal (e.g. `"metrics.jsonl"`) — a fixed filename, not a computed one.
func isQuotedLiteral(expr string) bool {
	expr = strings.TrimSpace(expr)
	return len(expr) >= 2 && expr[0] == '"' && expr[len(expr)-1] == '"' && !strings.ContainsAny(expr[1:len(expr)-1], `"+`)
}

// dirExprFor renders a readable directory expression for dirVar: the
// filepath.Join(...) call it was assigned from, if found, else the bare
// variable name.
func dirExprFor(src, dirVar string) string {
	re := regexp.MustCompile(regexp.QuoteMeta(dirVar) + `\s*:?=\s*filepath\.Join\(([^)]*)\)`)
	if m := re.FindStringSubmatch(src); m != nil {
		return "filepath.Join(" + strings.TrimSpace(m[1]) + ")"
	}
	// Fall back to the general assignment matcher (handles some formatting
	// variance dirJoinAssignRe alone might catch that the exact-var regex
	// above misses).
	for _, m := range dirJoinAssignRe.FindAllStringSubmatch(src, -1) {
		if m[1] == dirVar {
			return "filepath.Join(" + strings.TrimSpace(m[2]) + ")"
		}
	}
	return dirVar
}
