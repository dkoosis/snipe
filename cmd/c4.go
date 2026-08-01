package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/c4"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// C4Cmd is the `snipe c4` CLI command: a facts-only C4 architecture
// inventory (containers, datastores, external systems, flows). Registered in
// cmd/cli.go next to Triage/Diagram/Lifecycle.
type C4Cmd struct {
	Level string `default:"container" enum:"context,container,component" help:"context|container|component (default: container)"`
}

// Run implements kong's command interface.
func (c *C4Cmd) Run() error {
	return runC4(c.Level)
}

// runC4 opens the store, builds the C4 fact inventory via internal/c4, and
// writes it — following cmd/triage.go's runTriage/buildTriageResponse split
// (the design field's explicit template pointer). Like triage, the text
// renderer bypasses output.Writer's writeClaude type switch entirely (that
// switch lives in internal/output/json.go, which is out of this change's
// file list): JSON mode encodes the standard Response[C4Result] envelope
// directly, text mode calls writeC4Text.
func runC4(level string) error {
	start := time.Now()
	level = normalizeC4Level(level)

	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, dir, err := OpenStore(w, cmdNameC4)
	if err != nil {
		return err
	}
	defer s.Close()

	resp, err := buildC4Response(s, dir, level, start)
	if err != nil {
		return w.WriteError(cmdNameC4, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if GetOutputFormat() == output.OutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	return writeC4Text(resp.Results)
}

// normalizeC4Level defaults an empty/unrecognized --level to "container" (the
// design field's default) rather than erroring — an unknown level is more
// useful degraded to the default than refused outright.
func normalizeC4Level(level string) string {
	switch level {
	case c4.LevelContext, c4.LevelComponent:
		return level
	default:
		return c4.LevelContainer
	}
}

// buildC4Response builds the C4 facts and wraps them in the standard
// Response[T] envelope (AC #2: `.results` is always an array, never null, via
// Response[T]'s existing MarshalJSON guarantee — no bespoke envelope).
func buildC4Response(s *store.Store, dir, level string, start time.Time) (output.Response[output.C4Result], error) {
	facts, err := c4.BuildFacts(s, dir, level)
	if err != nil {
		return output.Response[output.C4Result]{}, fmt.Errorf("build c4 facts: %w", err)
	}

	result := output.C4Result{
		Module:     facts.Module,
		GoVersion:  facts.GoVersion,
		Level:      level,
		Containers: make([]output.C4Container, 0, len(facts.Containers)),
		Datastores: make([]output.C4Datastore, 0, len(facts.Datastores)),
		External:   make([]output.C4ExternalSystem, 0, len(facts.External)),
		Flows:      make([]output.C4Flow, 0, len(facts.Flows)),
		Components: make([]output.C4Component, 0, len(facts.Components)),
	}
	for _, c := range facts.Containers {
		result.Containers = append(result.Containers, output.C4Container{
			Name: c.Name, Tech: c.Tech, File: c.File, Line: c.Line,
		})
	}
	for _, d := range facts.Datastores {
		result.Datastores = append(result.Datastores, output.C4Datastore{
			Name: d.Name, Driver: d.Driver, Evidence: d.Evidence, File: d.File, Line: d.Line,
		})
	}
	for _, e := range facts.External {
		result.External = append(result.External, output.C4ExternalSystem{
			Name: e.Name, Kind: e.Kind, Evidence: e.Evidence, File: e.File, Line: e.Line,
		})
	}
	for _, fl := range facts.Flows {
		result.Flows = append(result.Flows, output.C4Flow{
			From: fl.From, To: fl.To, Kind: fl.Kind, File: fl.File, Line: fl.Line,
		})
	}
	for _, comp := range facts.Components {
		result.Components = append(result.Components, output.C4Component{
			Name: comp.Name, Layer: comp.Layer, Purpose: comp.Purpose,
		})
	}

	total := len(result.Containers) + len(result.Datastores) + len(result.External) +
		len(result.Flows) + len(result.Components)

	return output.Response[output.C4Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.C4Result{result},
		Meta: output.Meta{
			Command:    cmdNameC4,
			Query:      map[string]string{"level": level},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      total,
		},
	}, nil
}

// c4TextCap bounds the concise renderer to roughly the design field's <=~80
// lines target on the snipe repo, per section.
const c4TextCap = 20

// writeC4Text renders the Claude-optimized concise form: one section per
// fact kind, one line per fact (name | tech/evidence | file:line).
func writeC4Text(results []output.C4Result) error {
	if len(results) == 0 {
		_, err := os.Stdout.WriteString("c4 · no facts\n")
		return err
	}
	r := results[0]

	var b strings.Builder
	fmt.Fprintf(&b, "c4 · %s · level=%s", r.Module, r.Level)
	if r.GoVersion != "" {
		fmt.Fprintf(&b, " · go=%s", r.GoVersion)
	}
	b.WriteString("\n")

	if len(r.Containers) > 0 {
		b.WriteString("\n## containers\n")
		writeC4Capped(&b, len(r.Containers), func(i int) string {
			c := r.Containers[i]
			return fmt.Sprintf("  %s | %s | %s:%d\n", c.Name, c.Tech, c.File, c.Line)
		})
	}

	if len(r.Datastores) > 0 {
		b.WriteString("\n## datastores\n")
		writeC4Capped(&b, len(r.Datastores), func(i int) string {
			d := r.Datastores[i]
			return fmt.Sprintf("  %s | %s | evidence: %s | %s:%d\n",
				d.Name, d.Driver, strings.Join(d.Evidence, ", "), d.File, d.Line)
		})
	}

	if len(r.External) > 0 {
		b.WriteString("\n## external\n")
		writeC4Capped(&b, len(r.External), func(i int) string {
			e := r.External[i]
			return fmt.Sprintf("  %s | %s | evidence: %s | %s:%d\n",
				e.Name, e.Kind, strings.Join(e.Evidence, ", "), e.File, e.Line)
		})
	}

	if len(r.Flows) > 0 {
		b.WriteString("\n## flows\n")
		writeC4Capped(&b, len(r.Flows), func(i int) string {
			fl := r.Flows[i]
			from := fl.From
			if from == "" {
				from = "(env)"
			}
			return fmt.Sprintf("  %s -> %s | %s | %s:%d\n", from, fl.To, fl.Kind, fl.File, fl.Line)
		})
	}

	if len(r.Components) > 0 {
		b.WriteString("\n## components\n")
		writeC4Capped(&b, len(r.Components), func(i int) string {
			c := r.Components[i]
			if c.Purpose != "" {
				return fmt.Sprintf("  %s (%s) — %s\n", c.Name, c.Layer, c.Purpose)
			}
			return fmt.Sprintf("  %s (%s)\n", c.Name, c.Layer)
		})
	}

	if len(r.Containers) == 0 && len(r.Datastores) == 0 && len(r.External) == 0 &&
		len(r.Flows) == 0 && len(r.Components) == 0 {
		b.WriteString("(no facts found)\n")
	}

	_, err := os.Stdout.WriteString(b.String())
	return err
}

// writeC4Capped writes at most c4TextCap lines from render(i), i in [0,n),
// appending a "+N more" marker when truncated.
func writeC4Capped(b *strings.Builder, n int, render func(i int) string) {
	limit := n
	if limit > c4TextCap {
		limit = c4TextCap
	}
	for i := 0; i < limit; i++ {
		b.WriteString(render(i))
	}
	if n > limit {
		fmt.Fprintf(b, "  ... +%d more\n", n-limit)
	}
}
