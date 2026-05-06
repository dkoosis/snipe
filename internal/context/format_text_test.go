package context

import (
	"strings"
	"testing"
)

func TestFormatText_TriageLine(t *testing.T) {
	t.Run("emits triage line when IndexState set", func(t *testing.T) {
		bc := &BootContext{
			Project:      "testproj",
			TotalSymbols: 42,
			TotalPkgs:    7,
			IndexState:   "fresh",
		}
		out := FormatText(bc)
		want := "42 symbols | 7 pkgs | index: fresh"
		if !strings.HasPrefix(out, want) {
			t.Errorf("expected output to start with %q, got:\n%s", want, out[:min(len(out), 80)])
		}
	})

	t.Run("emits stale sigil when IndexState is stale", func(t *testing.T) {
		bc := &BootContext{
			Project:      "testproj",
			TotalSymbols: 12,
			TotalPkgs:    8,
			IndexState:   "stale",
		}
		out := FormatText(bc)
		want := "12 symbols | 8 pkgs | ! index: stale"
		if !strings.HasPrefix(out, want) {
			t.Errorf("expected output to start with %q, got:\n%s", want, out[:min(len(out), 80)])
		}
	})

	t.Run("omits triage line when IndexState is empty", func(t *testing.T) {
		bc := &BootContext{Project: "testproj", TotalSymbols: 5, TotalPkgs: 2}
		out := FormatText(bc)
		if strings.Contains(out, "symbols | ") {
			t.Error("expected no triage line when IndexState is empty")
		}
	})
}

func TestFormatText_DepDAG(t *testing.T) {
	t.Run("emits deps section when DAG present", func(t *testing.T) {
		bc := &BootContext{
			Project: "testproj",
			DepDAG: &DepDAG{
				Edges: []DepEdge{
					{From: "cmd", To: []string{"internal/query", "internal/store"}},
					{From: "internal/query", To: []string{"internal/store"}},
				},
			},
		}
		out := FormatText(bc)
		if !strings.Contains(out, "## deps") {
			t.Error("expected ## deps section in output")
		}
		if !strings.Contains(out, "cmd → internal/query internal/store") {
			t.Error("expected cmd edge in deps output")
		}
		if !strings.Contains(out, "internal/query → internal/store") {
			t.Error("expected internal/query edge in deps output")
		}
	})

	t.Run("omits deps section when DAG is nil", func(t *testing.T) {
		bc := &BootContext{Project: "testproj"}
		out := FormatText(bc)
		if strings.Contains(out, "## deps") {
			t.Error("expected no ## deps section when DepDAG is nil")
		}
	})

	t.Run("omits deps section when DAG has no edges", func(t *testing.T) {
		bc := &BootContext{Project: "testproj", DepDAG: &DepDAG{}}
		out := FormatText(bc)
		if strings.Contains(out, "## deps") {
			t.Error("expected no ## deps section when DepDAG.Edges is empty")
		}
	})

	t.Run("renders arch warnings sorted with metrics", func(t *testing.T) {
		bc := &BootContext{
			Project: "testproj",
			ArchWarnings: []ArchWarning{
				{Pkg: "internal/config", A: 0.00, I: 0.00, D: 1.00},
				{Pkg: "internal/output", A: 0.00, I: 0.17, D: 0.83},
			},
		}
		out := FormatText(bc)
		if !strings.Contains(out, "## arch warnings") {
			t.Error("expected ## arch warnings section")
		}
		if !strings.Contains(out, "internal/config — A=0.00 I=0.00 D=1.00") {
			t.Errorf("expected formatted row for config, got:\n%s", out)
		}
	})

	t.Run("omits arch warnings section when empty", func(t *testing.T) {
		bc := &BootContext{Project: "testproj"}
		out := FormatText(bc)
		if strings.Contains(out, "## arch warnings") {
			t.Error("expected no ## arch warnings when empty")
		}
	})
}
