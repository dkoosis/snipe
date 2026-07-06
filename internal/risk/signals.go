package risk

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	snipecontext "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// Signal-strength cutoffs. Centrality mirrors the ccp-1lk5 spike (top-5 → +2,
// top-20 → +1); blast fan-out and role weights follow the plan.
const (
	centralTopStrong = 5
	centralTopWeak   = 20
	churnTopN        = 20
	blastCallersHigh = 20
	blastFilesHigh   = 8
	blastCallersMed  = 5
	blastCallerCap   = 500

	weightStrong = 2
	weightWeak   = 1
)

// Signal codes (stable identifiers in the JSON contract).
const (
	sigCentral    = "central"
	sigChurn      = "churn-hotspot"
	sigBlast      = "blast"
	sigRolePrefix = "role:"
	sigRiskPrefix = "risk:"
)

// changedSym is a symbol a diff touched, with the package it belongs to.
type changedSym struct {
	id      string
	pkgPath string
}

// Assess runs the full pipeline for `snipe risk`: diff → changed symbols → gather
// the code-graph signals snipe owns → Fuse into a verdict. It never errors — when
// git or the index can't answer, it returns a degraded low verdict so the judge can
// call it on any repo.
func Assess(s *store.Store, repoRoot, base, head string) Verdict {
	if s == nil {
		return Fuse(nil, ChangeStats{}, true, "index unavailable")
	}
	stream, ok := gitDiff(repoRoot, base, head)
	if !ok {
		return Fuse(nil, ChangeStats{}, true,
			"git diff unavailable (not a work tree, unresolved ref, or git absent)")
	}
	changes := parseDiff(stream)
	goFiles := changedGoFiles(changes)
	stats := ChangeStats{Files: len(changes), GoFiles: len(goFiles)}
	if len(goFiles) == 0 {
		return Fuse(nil, stats, true, "no changed Go files")
	}

	changed := changedSymbols(s.DB(), repoRoot, changes)
	stats.Symbols = len(changed)

	var signals []Signal
	signals = append(signals, centralSignal(s, changed)...)
	signals = append(signals, churnSignal(s, goFiles)...)
	signals = append(signals, roleSignals(s.DB(), repoRoot, changed)...)
	signals = append(signals, blastSignal(s.DB(), changed)...)

	return Fuse(signals, stats, false, "")
}

// changedSymbols maps changed files to the func/method/type symbols whose body
// overlaps a changed head-side line range. Symbols the index doesn't know (e.g. a
// brand-new file not yet reindexed) simply don't appear.
func changedSymbols(db *sql.DB, repoRoot string, changes []FileChange) []changedSym {
	var out []changedSym
	seen := make(map[string]bool)
	for _, fc := range changes {
		// Symbol-derived signals (role/blast/central-pkg) mean production risk;
		// a _test.go edit shouldn't carry a persistence role or blast radius, so it
		// contributes no changed symbols (its churn/pkg still count elsewhere).
		if !strings.HasSuffix(fc.Path, ".go") || strings.HasSuffix(fc.Path, "_test.go") || len(fc.LineRanges) == 0 {
			continue
		}
		abs := filepath.Join(repoRoot, fc.Path)
		rows, err := db.Query(`
			SELECT id, pkg_path, line_start, line_end
			FROM symbols
			WHERE file_path = ? AND kind IN ('func', 'method', 'struct', 'interface', 'type')
		`, abs)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id string
			var pkg sql.NullString
			var start, end int
			if err := rows.Scan(&id, &pkg, &start, &end); err != nil {
				continue
			}
			if seen[id] || !overlaps(start, end, fc.LineRanges) {
				continue
			}
			seen[id] = true
			out = append(out, changedSym{id: id, pkgPath: pkg.String})
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

// centralSignal fires when a changed file's package is import-central (PageRank).
// The best (lowest) rank across all changed packages sets the weight.
func centralSignal(s *store.Store, changed []changedSym) []Signal {
	pkgs := distinctPkgs(changed)
	if len(pkgs) == 0 {
		return nil
	}
	rows, err := s.ReadTopN("imports", "pagerank", 0)
	if err != nil || len(rows) == 0 {
		return nil
	}
	rank := make(map[string]int, len(rows))
	for _, r := range rows {
		rank[r.NodeID] = r.Rank
	}
	best, bestPkg := 0, ""
	for _, p := range pkgs {
		if r, ok := rank[p]; ok && (best == 0 || r < best) {
			best, bestPkg = r, p
		}
	}
	weight := 0
	switch {
	case best == 0:
		return nil
	case best <= centralTopStrong:
		weight = weightStrong
	case best <= centralTopWeak:
		weight = weightWeak
	default:
		return nil
	}
	return []Signal{{
		Signal: sigCentral,
		Detail: fmt.Sprintf("%s ranks #%d/%d by PageRank", bestPkg, best, len(rows)),
		Weight: weight,
	}}
}

// churnSignal fires when a changed file sits in the churn×complexity hotspot top-N.
func churnSignal(s *store.Store, goFiles []string) []Signal {
	rows, err := s.ReadFileChurnTopN(churnTopN)
	if err != nil || len(rows) == 0 {
		return nil
	}
	hot := make(map[string]bool, len(rows))
	for _, r := range rows {
		hot[r.Path] = true
	}
	for _, f := range goFiles {
		if hot[f] {
			return []Signal{{
				Signal: sigChurn,
				Detail: fmt.Sprintf("%s in churn top-%d", f, churnTopN),
				Weight: weightWeak,
			}}
		}
	}
	return nil
}

// roleSignals fires per distinct architectural role / risk class among the changed
// symbols. Persistence & api_boundary weigh strong; entry_point weak; each
// concurrency / security_boundary risk flag weighs strong. Each class fires once.
func roleSignals(db *sql.DB, repoRoot string, changed []changedSym) []Signal {
	ids := symbolIDs(changed)
	if len(ids) == 0 {
		return nil
	}
	roles, err := snipecontext.RolesForSymbols(db, repoRoot, ids)
	if err != nil {
		return nil
	}
	roleCount := make(map[snipecontext.Role]int)
	riskCount := make(map[string]int)
	for _, sr := range roles {
		roleCount[sr.Role]++
		for _, rf := range sr.RiskFlags {
			riskCount[rf]++
		}
	}

	var sigs []Signal
	emitRole := func(role snipecontext.Role, weight int) {
		if n := roleCount[role]; n > 0 {
			sigs = append(sigs, Signal{
				Signal: sigRolePrefix + string(role),
				Detail: fmt.Sprintf("%d changed symbol(s) in a %s role", n, role),
				Weight: weight,
			})
		}
	}
	emitRole(snipecontext.RolePersistence, weightStrong)
	emitRole(snipecontext.RoleAPIBoundary, weightStrong)
	emitRole(snipecontext.RoleEntryPoint, weightWeak)

	for _, rf := range []string{snipecontext.RiskConcurrency, snipecontext.RiskSecurityBoundary} {
		if n := riskCount[rf]; n > 0 {
			sigs = append(sigs, Signal{
				Signal: sigRiskPrefix + rf,
				Detail: fmt.Sprintf("%d changed symbol(s) touch %s", n, rf),
				Weight: weightStrong,
			})
		}
	}
	return sigs
}

// blastSignal fires on wide caller fan-out — a change to code many other sites call.
func blastSignal(db *sql.DB, changed []changedSym) []Signal {
	ids := symbolIDs(changed)
	if len(ids) == 0 {
		return nil
	}
	rows, err := query.FindImpactCallersMulti(db, ids, true, blastCallerCap, 0)
	if err != nil || len(rows) == 0 {
		return nil
	}
	files := make(map[string]bool)
	for i := range rows {
		files[rows[i].FilePathRel] = true
	}
	callers := len(rows)
	weight := 0
	switch {
	case callers >= blastCallersHigh && len(files) >= blastFilesHigh:
		weight = weightStrong
	case callers >= blastCallersMed:
		weight = weightWeak
	default:
		return nil
	}
	return []Signal{{
		Signal: sigBlast,
		Detail: fmt.Sprintf("%d callers across %d files", callers, len(files)),
		Weight: weight,
	}}
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
