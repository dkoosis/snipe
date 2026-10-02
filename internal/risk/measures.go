package risk

import (
	"cmp"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	snipecontext "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

const (
	callerCap = 500 // bounds the caller walk so a change to a hub symbol stays fast
	focusMax  = 3
)

// changedSym is a production symbol a diff touched.
type changedSym struct {
	id, name, kind, pkgPath, fileRel, receiver string
	lineStart, lineEnd                         int
}

func (c changedSym) isFunc() bool { return c.kind == "func" || c.kind == "method" }

// Assess measures the diff between base and head and scores it. It never
// errors — when git or the index can't answer, it returns a degraded result.
func Assess(s *store.Store, repoRoot, base, head string) Measures {
	if s == nil {
		return Degrade(ChangeStats{}, "index unavailable")
	}
	stream, ok := gitDiff(repoRoot, base, head)
	if !ok {
		return Degrade(ChangeStats{}, "git diff unavailable (not a work tree, unresolved ref, or git absent)")
	}
	changes := parseDiff(stream)
	goFiles := changedGoFiles(changes)
	stats := ChangeStats{Files: len(changes), GoFiles: len(goFiles)}
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_test.go") {
			stats.TestFiles++
		}
	}
	stats.CodeLinesAdded, stats.CodeLinesRemoved = codeLines(repoRoot, base, head)
	if len(goFiles) == 0 {
		return Degrade(stats, "no changed Go files")
	}

	db := s.DB()
	changed := changedSymbols(db, repoRoot, changes)
	stats.Symbols = len(changed)

	m := Measures{Changed: stats}
	m.Reach = reach(s, changed)
	cx := complexity(repoRoot, head, changes)
	m.Difficulty = difficulty(cx)
	m.History = history(s, goFiles)
	m.Kind = kind(db, repoRoot, changed)
	tested := testedFuncs(db, changed)
	m.Tests = tests(changed, tested)
	m.Focus = focus(s, changed, cx, tested)
	m.score()
	return m
}

// changedSymbols maps changed files to the func/method/type symbols whose body
// overlaps a changed head-side line range. Symbols the index doesn't know (e.g. a
// brand-new file not yet reindexed) simply don't appear.
func changedSymbols(db *sql.DB, repoRoot string, changes []FileChange) []changedSym {
	var out []changedSym
	seen := make(map[string]bool)
	for _, fc := range changes {
		// A _test.go edit is not a change to production code: it contributes
		// no symbols (its file still counts in Changed and History).
		if !strings.HasSuffix(fc.Path, ".go") || strings.HasSuffix(fc.Path, "_test.go") || len(fc.LineRanges) == 0 {
			continue
		}
		rows, err := db.Query(`
			SELECT id, name, kind, pkg_path, COALESCE(receiver, ''), line_start, line_end
			FROM symbols
			WHERE file_path = ? AND kind IN ('func', 'method', 'struct', 'interface', 'type')
		`, filepath.Join(repoRoot, fc.Path))
		if err != nil {
			continue
		}
		for rows.Next() {
			c := changedSym{fileRel: fc.Path}
			var pkg sql.NullString
			if err := rows.Scan(&c.id, &c.name, &c.kind, &pkg, &c.receiver, &c.lineStart, &c.lineEnd); err != nil {
				continue
			}
			if seen[c.id] || !overlaps(c.lineStart, c.lineEnd, fc.LineRanges) {
				continue
			}
			seen[c.id] = true
			c.pkgPath = pkg.String
			out = append(out, c)
		}
		_ = rows.Close()
	}
	return out
}

// overlaps reports whether [start,end] intersects any of the ranges.
func overlaps(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if start <= r[1] && r[0] <= end {
			return true
		}
	}
	return false
}

func reach(s *store.Store, changed []changedSym) Reach {
	var r Reach
	db := s.DB()
	if ids := symbolIDs(changed); len(ids) > 0 {
		if rows, err := query.FindImpactCallersMulti(db, ids, true, callerCap, 0); err == nil {
			files := make(map[string]bool)
			for i := range rows {
				files[rows[i].FilePathRel] = true
			}
			r.Callers, r.CallerFiles = len(rows), len(files)
		}
	}
	pkgs := distinctPkgs(changed)
	for _, p := range pkgs {
		var n int
		if err := db.QueryRow(`
			SELECT COUNT(DISTINCT importer_pkg) FROM imports
			WHERE pkg_path = ? AND importer_pkg IS NOT NULL AND importer_pkg != ?
		`, p, p).Scan(&n); err == nil {
			r.Importers = max(r.Importers, n)
		}
	}
	if ranked, err := s.ReadTopN("imports", "pagerank", 0); err == nil && len(ranked) > 0 {
		rank := make(map[string]int, len(ranked))
		for _, row := range ranked {
			rank[row.NodeID] = row.Rank
		}
		for _, p := range pkgs {
			if n, ok := rank[p]; ok && (r.PkgRank == nil || n < *r.PkgRank) {
				r.PkgRank = &n
			}
		}
		r.PkgCount = len(ranked)
	}
	return r
}

// funcCx is one function's complexity at the head of the diff.
type funcCx struct {
	name             string
	line             int // line of the function name
	cyclo, cognitive int
}

// complexity parses each changed production Go file as it is at head and
// measures the functions a changed line touches. The index holds only file and
// package rollups, which a one-line fix in a large file would dilute.
func complexity(repoRoot, head string, changes []FileChange) map[string][]funcCx {
	out := map[string][]funcCx{}
	for _, fc := range changes {
		if !strings.HasSuffix(fc.Path, ".go") || strings.HasSuffix(fc.Path, "_test.go") || len(fc.LineRanges) == 0 {
			continue
		}
		src, err := exec.Command("git", "-C", repoRoot, "show", "--end-of-options", head+":"+fc.Path).Output()
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, fc.Path, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || !overlaps(fset.Position(fd.Pos()).Line, fset.Position(fd.End()).Line, fc.LineRanges) {
				continue
			}
			out[fc.Path] = append(out[fc.Path], funcCx{
				name: fd.Name.Name, line: fset.Position(fd.Name.Pos()).Line,
				cyclo: index.Cyclo(fd.Body), cognitive: index.Cognitive(fd.Body),
			})
		}
	}
	return out
}

// cxFor finds a changed function's complexity: same file, same name, its name
// line inside the symbol's range.
func cxFor(c changedSym, cx map[string][]funcCx) (funcCx, bool) {
	for _, f := range cx[c.fileRel] {
		if f.name == c.name && f.line >= c.lineStart && f.line <= c.lineEnd {
			return f, true
		}
	}
	return funcCx{}, false
}

func difficulty(cx map[string][]funcCx) Difficulty {
	var d Difficulty
	for _, fs := range cx {
		for _, f := range fs {
			if d.CycloMax == nil {
				d.CycloMax, d.CognitiveMax = new(int), new(int)
			}
			*d.CycloMax = max(*d.CycloMax, f.cyclo)
			*d.CognitiveMax = max(*d.CognitiveMax, f.cognitive)
		}
	}
	return d
}

// history is the churn of the changed Go files, the most for any one file.
func history(s *store.Store, goFiles []string) *History {
	rows, err := s.ReadFileChurnRanked(store.ChurnRankCommits, 0)
	if err != nil || len(rows) == 0 {
		return nil
	}
	byPath := make(map[string]store.FileChurn, len(rows))
	for _, r := range rows {
		byPath[r.Path] = r
	}
	h := &History{}
	for _, f := range goFiles {
		r := byPath[f]
		h.Commits = max(h.Commits, r.Commits)
		h.BugCommits = max(h.BugCommits, r.BugCommits)
		h.Churn = max(h.Churn, r.Score)
		h.Authors = max(h.Authors, r.Authors)
	}
	return h
}

func kind(db *sql.DB, repoRoot string, changed []changedSym) Kind {
	k := Kind{Roles: map[string]int{}, RiskFlags: map[string]int{}}
	for _, c := range changed {
		if r := []rune(c.name); len(r) > 0 && unicode.IsUpper(r[0]) {
			k.Exported++
		}
	}
	ids := symbolIDs(changed)
	if len(ids) == 0 {
		return k
	}
	got, err := snipecontext.RolesForSymbols(db, repoRoot, ids)
	if err != nil {
		return k
	}
	for _, sr := range got {
		if sr.Role != "" {
			k.Roles[string(sr.Role)]++
		}
		for _, rf := range sr.RiskFlags {
			k.RiskFlags[rf]++
		}
	}
	return k
}

// testedFuncs is the set of changed function IDs some test reaches.
func testedFuncs(db *sql.DB, changed []changedSym) map[string]bool {
	out := map[string]bool{}
	for _, c := range changed {
		if !c.isFunc() {
			continue
		}
		if rows, err := query.FindTests(db, c.id, false, 1, 0); err == nil && len(rows) > 0 {
			out[c.id] = true
		}
	}
	return out
}

func tests(changed []changedSym, tested map[string]bool) Tests {
	t := Tests{Untested: []string{}}
	for _, c := range changed {
		if !c.isFunc() {
			continue
		}
		t.ChangedFuncs++
		if !tested[c.id] {
			t.Untested = append(t.Untested, qualified(c))
		}
	}
	slices.Sort(t.Untested)
	return t
}

// focus is the changed functions a reviewer should look at first: most direct
// callers, then most complex.
func focus(s *store.Store, changed []changedSym, cx map[string][]funcCx, tested map[string]bool) []Focus {
	inDeg := map[string]int{}
	if rows, err := s.ReadTopN("calls", "in_degree", 0); err == nil {
		for _, r := range rows {
			inDeg[r.NodeID] = int(r.Value)
		}
	}
	out := []Focus{}
	for _, c := range changed {
		if !c.isFunc() {
			continue
		}
		f := Focus{Name: qualified(c), File: c.fileRel, Line: c.lineStart, Callers: inDeg[c.id], Tested: tested[c.id]}
		if x, ok := cxFor(c, cx); ok {
			f.Cyclo, f.Cognitive = x.cyclo, x.cognitive
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b Focus) int {
		return cmp.Or(cmp.Compare(b.Callers, a.Callers), cmp.Compare(b.Cyclo, a.Cyclo),
			cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line))
	})
	return out[:min(len(out), focusMax)]
}

// qualified names a symbol as pkg.Name or pkg.Recv.Name, the way a reader
// would say it.
func qualified(c changedSym) string {
	name := c.name
	if recv := strings.Trim(c.receiver, "()*"); recv != "" {
		name = recv + "." + name
	}
	if c.pkgPath == "" {
		return name
	}
	return path.Base(c.pkgPath) + "." + name
}

// distinctPkgs returns the unique, non-empty package paths among changed symbols.
func distinctPkgs(changed []changedSym) []string {
	seen := make(map[string]bool)
	var out []string
	for _, c := range changed {
		if c.pkgPath != "" && !seen[c.pkgPath] {
			seen[c.pkgPath] = true
			out = append(out, c.pkgPath)
		}
	}
	return out
}

// symbolIDs returns the changed symbols' IDs.
func symbolIDs(changed []changedSym) []string {
	out := make([]string, len(changed))
	for i, c := range changed {
		out[i] = c.id
	}
	return out
}
