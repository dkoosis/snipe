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

// writeRiskText keeps the literal "degraded:" marker: cc-plugins' dispatch
// probe drops a degraded answer by matching it.
func writeRiskText(m risk.Measures) error {
	var b strings.Builder
	fmt.Fprintf(&b, "risk · %d files (%d go) · %d symbols\n", m.Changed.Files, m.Changed.GoFiles, m.Changed.Symbols)
	if m.Degraded {
		fmt.Fprintf(&b, "  degraded: %s\n", m.Note)
	} else {
		fmt.Fprintf(&b, "  callers %d in %d files · importers %d · commits %d\n",
			m.Callers, m.CallerFiles, m.Importers, m.Commits)
		if len(m.Roles)+len(m.RiskFlags) > 0 {
			fmt.Fprintf(&b, "  roles: %s · risk flags: %s\n", counts(m.Roles), counts(m.RiskFlags))
		}
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
