package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/risk"
)

// runRisk assesses the risk of the diff between two git refs and prints the verdict.
// It never errors on a missing index or non-git repo — those degrade to a low
// verdict — so the review-judge can call it uniformly on any repo.
func runRisk(base, head string) error {
	start := time.Now()

	// nil writer: suppress OpenStore's missing-index error envelope. Risk degrades
	// to a low verdict instead, still emitting a valid contract.
	s, root, err := OpenStore(nil, cmdNameRisk)
	if err != nil {
		v := risk.Fuse(nil, risk.ChangeStats{}, true, "index unavailable: "+err.Error())
		return writeRisk(v, root, protocol.IndexMissing, start)
	}
	defer s.Close()

	// A stale index maps the diff to too few symbols and reads as a small, safe
	// change, so a stale verdict is degraded: the caller must not route on it.
	v := risk.Assess(s, root, base, head)
	state := protocol.IndexFresh
	if why := indexStaleness(s, root); why != "" {
		state = protocol.IndexStale
		if !v.Degraded {
			v = risk.Fuse(nil, v.Changed, true, "index is stale: "+why+" — run: snipe index")
		}
	}
	return writeRisk(v, root, state, start)
}

func writeRisk(v risk.Verdict, root string, state protocol.IndexState, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		return writeRiskJSON(v, root, state, start)
	}
	return writeRiskText(v)
}

// writeRiskJSON emits the verdict inside the standard envelope. The single verdict
// is the sole result; consumers read `.results[0]` (e.g. `jq '.results[0].verdict'`).
// This shape is a semver-guarded cross-repo contract — see docs/contracts/risk-json.md
// (guard test: test/blackbox/risk_contract_test.go, sn-n8re).
func writeRiskJSON(v risk.Verdict, root string, state protocol.IndexState, start time.Time) error {
	resp := protocol.Response[risk.Verdict]{
		Protocol: protocol.ProtocolVersion,
		Ok:       true,
		Results:  []risk.Verdict{v},
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

func writeRiskText(v risk.Verdict) error {
	var b strings.Builder
	fmt.Fprintf(&b, "risk · %s (score %d)\n", v.Verdict, v.Score)
	if v.Degraded {
		fmt.Fprintf(&b, "  ⚠ degraded: %s\n", v.Note)
	}
	if len(v.Reasons) == 0 && !v.Degraded {
		b.WriteString("  (no risk signals)\n")
	}
	for _, r := range v.Reasons {
		fmt.Fprintf(&b, "  %-22s %s  (+%d)\n", r.Signal, r.Detail, r.Weight)
	}
	fmt.Fprintf(&b, "  changed: %d files · %d go · %d symbols\n",
		v.Changed.Files, v.Changed.GoFiles, v.Changed.Symbols)
	_, err := os.Stdout.WriteString(b.String())
	return err
}
