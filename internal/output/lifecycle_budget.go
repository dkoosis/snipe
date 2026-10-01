package output

import (
	"fmt"
	"strings"

	"github.com/dkoosis/snipe/internal/protocol"
)

// TruncateLifecycleToTokenBudget shrinks r to fit within maxTokens by dropping
// callers first (least informative), then tail functions from each group, then
// empty groups. Ordering within groups is preserved (stable).
// Returns the modified result and whether truncation occurred.
func TruncateLifecycleToTokenBudget(r protocol.LifecycleResult, maxTokens int) (protocol.LifecycleResult, bool) {
	if maxTokens <= 0 {
		return r, false
	}
	estimate := lifecycleTokenEstimate(r)
	if estimate <= maxTokens {
		return r, false
	}

	// Pass 1: drop all caller chains.
	stripped := r
	stripped.Groups = make([]protocol.LifecycleGroup, len(r.Groups))
	for i, g := range r.Groups {
		ng := g
		ng.Funcs = make([]protocol.LifecycleFunction, len(g.Funcs))
		for j, f := range g.Funcs {
			nf := f
			nf.Callers = nil
			ng.Funcs[j] = nf
		}
		stripped.Groups[i] = ng
	}
	if estimate = lifecycleTokenEstimate(stripped); estimate <= maxTokens {
		return stripped, true
	}

	// Pass 2: trim tail functions from groups (largest groups first).
	trimmed := stripped
	trimmed.Groups = make([]protocol.LifecycleGroup, len(stripped.Groups))
	copy(trimmed.Groups, stripped.Groups)
	for lifecycleTokenEstimate(trimmed) > maxTokens {
		// Find the group with the most funcs and drop its last entry.
		best := -1
		for i, g := range trimmed.Groups {
			if len(g.Funcs) > 0 && (best == -1 || len(g.Funcs) > len(trimmed.Groups[best].Funcs)) {
				best = i
			}
		}
		if best == -1 {
			break
		}
		g := trimmed.Groups[best]
		g.Funcs = g.Funcs[:len(g.Funcs)-1]
		g.Count = len(g.Funcs)
		trimmed.Groups[best] = g
	}
	return trimmed, true
}

// lifecycleTokenEstimate approximates token count for a LifecycleResult.
// Uses 4 chars ≈ 1 token heuristic.
func lifecycleTokenEstimate(r protocol.LifecycleResult) int {
	var b strings.Builder
	fmt.Fprintf(&b, "# Lifecycle: %s %s:%d %s\n%d refs across %d functions\n",
		r.Type, r.TypeFile, r.TypeLine, r.TypeKind, r.TotalRefs, r.FunctionRefs)
	for _, g := range r.Groups {
		fmt.Fprintf(&b, "## %s (%d)\n", g.Role, g.Count)
		for _, f := range g.Funcs {
			fmt.Fprintf(&b, "- %s  %s:%d\n    signal: %s\n", f.Name, f.File, f.Line, f.Signal)
			if chain := lifecycleCallerChain(f.Callers); chain != "" {
				fmt.Fprintf(&b, "    callers: %s\n", chain)
			}
		}
	}
	return b.Len() / 4
}
