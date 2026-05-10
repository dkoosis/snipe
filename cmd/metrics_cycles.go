package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

// sccGroup is one nontrivial SCC in --kind=cycles output.
type sccGroup struct {
	ID       int      `json:"id"`
	Size     int      `json:"size"`
	Nodes    []string `json:"nodes"`
	SelfLoop bool     `json:"self_loop,omitempty"`
}

// cyclesPayload is the top-level results object for --kind=cycles JSON.
type cyclesPayload struct {
	SCCs []sccGroup `json:"sccs"`
}

// runCyclesMetrics reads graph_sccs and prints/serializes nontrivial cycles.
func runCyclesMetrics(s *store.Store, dir string, start time.Time) error {
	rs, err := s.ReadSCCs(metricsGraph)
	if err != nil {
		return fmt.Errorf("read sccs: %w", err)
	}

	groupMap := make(map[int]*sccGroup)
	var order []int
	for _, r := range rs {
		g, ok := groupMap[r.SCCID]
		if !ok {
			g = &sccGroup{ID: r.SCCID, Size: r.SCCSize}
			groupMap[r.SCCID] = g
			order = append(order, r.SCCID)
		}
		g.Nodes = append(g.Nodes, r.NodeID)
	}
	groups := make([]sccGroup, 0, len(order))
	for _, id := range order {
		g := groupMap[id]
		if g.Size == 1 && len(g.Nodes) == 1 {
			g.SelfLoop = true
		}
		groups = append(groups, *g)
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[cyclesPayload]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []cyclesPayload{{SCCs: groups}},
			Meta: output.Meta{
				Command:  cmdNameMetrics,
				Query:    map[string]string{"graph": metricsGraph, jsonKeyKind: cmdKindCycles},
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    len(groups),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s graph · cycles · %d nontrivial SCCs\n", metricsGraph, len(groups))
	if len(groups) == 0 {
		b.WriteString("  (no nontrivial SCCs — graph is acyclic, or run `snipe index` to populate)\n")
	}
	for i, g := range groups {
		suffix := ""
		if g.SelfLoop {
			suffix = "  (self-loop)"
		}
		fmt.Fprintf(&b, "  [%d] size=%d%s\n", i+1, g.Size, suffix)
		for _, n := range g.Nodes {
			fmt.Fprintf(&b, "      - %s\n", n)
		}
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}
