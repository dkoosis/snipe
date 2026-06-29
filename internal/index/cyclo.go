package index

import (
	"sort"
	"strings"
)

// CycloRollup is the per-package cyclomatic complexity summary.
// Sum/Max are integers; P95 may fall between samples (linear interpolation).
type CycloRollup struct {
	Sum int
	P95 float64
	Max int
	N   int // number of funcs/methods contributing
}

// PackageCycloRollups aggregates per-function cyclomatic complexity into
// per-package sum/p95/max. Filters production funcs+methods only (no tests,
// no zero-cyclo entries from non-function symbols). Empty packages → no entry.
func PackageCycloRollups(symbols []Symbol) map[string]CycloRollup {
	return packageRollups(symbols, func(s *Symbol) int { return s.Cyclo })
}

// PackageCognitiveRollups is the cognitive-complexity counterpart of
// PackageCycloRollups, using the same per-package sum/p95/max shape.
func PackageCognitiveRollups(symbols []Symbol) map[string]CycloRollup {
	return packageRollups(symbols, func(s *Symbol) int { return s.Cognitive })
}

// packageRollups groups production funcs+methods by package and rolls up the
// per-symbol value selected by val into sum/p95/max. Test files and
// non-function symbols are excluded; empty packages produce no entry.
func packageRollups(symbols []Symbol, val func(*Symbol) int) map[string]CycloRollup {
	byPkg := make(map[string][]int)
	for i := range symbols {
		s := &symbols[i]
		if s.Kind != KindFunc && s.Kind != KindMethod {
			continue
		}
		if s.PkgPath == "" {
			continue
		}
		if isTestFile(s.FilePath) {
			continue
		}
		byPkg[s.PkgPath] = append(byPkg[s.PkgPath], val(s))
	}
	out := make(map[string]CycloRollup, len(byPkg))
	for pkg, vals := range byPkg {
		out[pkg] = rollup(vals)
	}
	return out
}

func rollup(vals []int) CycloRollup {
	sort.Ints(vals)
	r := CycloRollup{N: len(vals), Max: vals[len(vals)-1]}
	for _, v := range vals {
		r.Sum += v
	}
	r.P95 = percentile(vals, 0.95)
	return r
}

// percentile returns the p-th (0..1) percentile of a sorted ascending slice
// using linear interpolation between the two nearest ranks. n=1 → vals[0].
func percentile(sorted []int, p float64) float64 {
	n := len(sorted)
	if n == 1 {
		return float64(sorted[0])
	}
	pos := p * float64(n-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= n {
		return float64(sorted[n-1])
	}
	frac := pos - float64(lo)
	return float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo])
}

func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go")
}
