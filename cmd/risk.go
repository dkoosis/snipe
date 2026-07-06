package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
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
		return writeRisk(v, root, start)
	}
	defer s.Close()

	v := risk.Assess(s, root, base, head)
	return writeRisk(v, root, start)
}

func writeRisk(v risk.Verdict, root string, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		return writeRiskJSON(v, root, start)
	}
	return writeRiskText(v)
}

// writeRiskJSON emits the verdict inside the standard envelope. The single verdict
// is the sole result; consumers read `.results[0]` (e.g. `jq '.results[0].verdict'`).
func writeRiskJSON(v risk.Verdict, root string, start time.Time) error {
	resp := output.Response[risk.Verdict]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []risk.Verdict{v},
		Meta: output.Meta{
			Command:  cmdNameRisk,
			RepoRoot: root,
			Ms:       time.Since(start).Milliseconds(),
			Total:    1,
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
