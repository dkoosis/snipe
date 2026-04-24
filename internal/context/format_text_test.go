package context

import (
	"strings"
	"testing"
)

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
}
