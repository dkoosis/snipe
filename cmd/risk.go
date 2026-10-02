package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/risk"
)

// runRisk measures the diff between two git refs and prints the measures.
// It never errors on a missing index or non-git repo — those degrade — so a
// caller can run it uniformly on any repo.
func runRisk(base, head string) error {
	start := time.Now()

	// nil writer: suppress OpenStore's missing-index error envelope. Risk
	// degrades instead, still emitting a valid contract.
	s, root, err := OpenStore(nil, cmdNameRisk)
	if err != nil {
		m := risk.Degrade(risk.ChangeStats{}, "index unavailable: "+err.Error())
		return writeRisk(m, root, protocol.IndexMissing, start)
	}
	defer s.Close()

	// A stale index maps the diff to too few symbols, so its measures read as
	// a small, safe change; a caller must not route on them.
	m := risk.Assess(s, root, base, head)
	state := protocol.IndexFresh
	if why := indexStaleness(s, root); why != "" {
		state = protocol.IndexStale
		if !m.Degraded {
			m = risk.Degrade(m.Changed, "index is stale: "+why+" — run: snipe index")
		}
	}
	return writeRisk(m, root, state, start)
}

func writeRisk(m risk.Measures, root string, state protocol.IndexState, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		return writeRiskJSON(m, root, state, start)
	}
	return writeRiskText(m)
}

// writeRiskJSON emits the measures inside the standard envelope as the sole
// result; consumers read `.results[0]`. This shape is a semver-guarded
// cross-repo contract — see docs/contracts/risk-json.md (guard test:
// test/blackbox/risk_contract_test.go).
func writeRiskJSON(m risk.Measures, root string, state protocol.IndexState, start time.Time) error {
	resp := protocol.Response[risk.Measures]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results:  []risk.Measures{m},
		Meta: protocol.Meta{
			Command:    cmdNameRisk,
			RepoRoot:   root,
			IndexState: state,
			Ms:         time.Since(start).Milliseconds(),
			Total:      1,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

// untestedShown caps the untested names in text output; JSON carries all.
const untestedShown = 5

// writeRiskText keeps the literal "degraded:" marker: cc-plugins' dispatch
// probe drops a degraded answer by matching it.
func writeRiskText(m risk.Measures) error {
	var b strings.Builder
	c := m.Changed
	size := fmt.Sprintf("%d files (%d go, %d test) · %d symbols · +%d/-%d code lines",
		c.Files, c.GoFiles, c.TestFiles, c.Symbols, c.CodeLinesAdded, c.CodeLinesRemoved)
	if m.Degraded || m.Score == nil {
		fmt.Fprintf(&b, "risk · %s\n  degraded: %s\n", size, m.Note)
		_, err := os.Stdout.WriteString(b.String())
		return err
	}

	fmt.Fprintf(&b, "risk %.2f ·", *m.Score)
	for _, f := range []string{risk.FactorReach, risk.FactorDifficulty, risk.FactorHistory, risk.FactorKind, risk.FactorSafetyNet} {
		if v, ok := m.Factors[f]; ok {
			fmt.Fprintf(&b, " %s %.2f", f, v)
		} else {
			fmt.Fprintf(&b, " %s n/a", f)
		}
	}
	fmt.Fprintf(&b, "  (weights v%d)\n", m.WeightsVersion)
	fmt.Fprintf(&b, "  size: %s\n", size)

	r := m.Reach
	fmt.Fprintf(&b, "  reach: %d callers in %d files · %d importing packages", r.Callers, r.CallerFiles, r.Importers)
	if r.PkgRank != nil {
		fmt.Fprintf(&b, " · package #%d of %d by PageRank", *r.PkgRank, r.PkgCount)
	}
	b.WriteString("\n")
	if d := m.Difficulty; d.CycloMax != nil {
		fmt.Fprintf(&b, "  difficulty: cyclomatic max %d · cognitive max %d\n", *d.CycloMax, *d.CognitiveMax)
	}
	if h := m.History; h != nil {
		fmt.Fprintf(&b, "  history: %d commits · %d bug fixes · churn %.1f · %d author%s\n",
			h.Commits, h.BugCommits, h.Churn, h.Authors, plural(h.Authors))
	}
	fmt.Fprintf(&b, "  kind: roles %s · risk flags %s · %d exported\n", counts(m.Kind.Roles), counts(m.Kind.RiskFlags), m.Kind.Exported)
	if t := m.Tests; t.ChangedFuncs > 0 {
		fmt.Fprintf(&b, "  tests: %d of %d changed funcs untested", len(t.Untested), t.ChangedFuncs)
		if n := len(t.Untested); n > 0 {
			shown := t.Untested[:min(n, untestedShown)]
			fmt.Fprintf(&b, ": %s", strings.Join(shown, ", "))
			if n > len(shown) {
				fmt.Fprintf(&b, " and %d more", n-len(shown))
			}
		}
		b.WriteString("\n")
	}
	if len(m.Focus) > 0 {
		parts := make([]string, len(m.Focus))
		for i, f := range m.Focus {
			parts[i] = fmt.Sprintf("%s %s:%d (%d callers, cyclo %d)", f.Name, f.File, f.Line, f.Callers, f.Cyclo)
		}
		fmt.Fprintf(&b, "  look first at: %s\n", strings.Join(parts, " · "))
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}

// counts renders a name→count map as "a 2, b 1", most first, then by name.
func counts(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	names := slices.Collect(maps.Keys(m))
	slices.SortFunc(names, func(a, b string) int {
		if m[a] != m[b] {
			return m[b] - m[a]
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s %d", n, m[n])
	}
	return strings.Join(parts, ", ")
}
