package diagram

import (
	"strings"
	"testing"
)

func TestQuoteEscaping(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", `"plain"`},
		{`with "quotes"`, `"with \"quotes\""`},
		{`back\slash`, `"back\\slash"`},
		{"line\nbreak", `"line\nbreak"`},
		{"github.com/dkoosis/snipe/internal/store", `"github.com/dkoosis/snipe/internal/store"`},
	}
	for _, c := range cases {
		got := quote(c.in)
		if got != c.want {
			t.Errorf("quote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "_"},
		{"simple", "simple"},
		{"github.com/foo", "github_com_foo"},
		{"with space", "with_space"},
		{"weird{}()", "weird____"},
		{"snake_case_ok", "snake_case_ok"},
	}
	for _, c := range cases {
		got := SanitizeID(c.in)
		if got != c.want {
			t.Errorf("SanitizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBuilder_NodesAndEdges(t *testing.T) {
	var b Builder
	b.Title = "demo"
	b.Direction = "right"
	b.AddNode("a", "Node A", "", nil)
	b.AddNode("b", "Node B", "circle", map[string]string{"fill": "#ddd"})
	b.AddEdge("a", "b", "calls", nil)

	out := b.Render()
	mustContain(t, out, "# demo")
	mustContain(t, out, "direction: right")
	mustContain(t, out, `a: "Node A"`)
	mustContain(t, out, `b: "Node B"`)
	mustContain(t, out, "shape: circle")
	mustContain(t, out, `style.fill: "#ddd"`)
	mustContain(t, out, `a -> b: "calls"`)
}

func TestBuilder_ContainerNesting(t *testing.T) {
	var b Builder
	b.AddContainer("layer.api", "API Layer", map[string]string{"fill": "#eef"})
	b.AddNode("layer.api.handler", "Handler", "", nil)
	b.AddNode("layer.store.db", "DB", "cylinder", nil)
	b.AddEdge("layer.api.handler", "layer.store.db", "writes", nil)

	out := b.Render()

	// Parent container auto-created via ensureParents.
	mustContain(t, out, `layer: "layer"`)
	// Explicit container with style.
	mustContain(t, out, `layer.api: "API Layer"`)
	mustContain(t, out, `style.fill: "#eef"`)
	// Auto-created container for layer.store (parent of layer.store.db).
	mustContain(t, out, `layer.store: "store"`)
	// Nodes referenced via dotted path.
	mustContain(t, out, `layer.api.handler: "Handler"`)
	mustContain(t, out, `layer.store.db: "DB"`)
	// Edge references full paths.
	mustContain(t, out, "layer.api.handler -> layer.store.db")
}

func TestBuilder_DashedEdge(t *testing.T) {
	var b Builder
	b.AddNode("a", "A", "", nil)
	b.AddNode("b", "B", "", nil)
	b.AddDashedEdge("a", "b", "optional")
	out := b.Render()
	mustContain(t, out, `a -> b: "optional"`)
	mustContain(t, out, "style.stroke-dash: 3")
}

func TestBuilder_DeterministicOrdering(t *testing.T) {
	// Containers in different insertion orders should produce the same output.
	mk := func(order []string) string {
		var b Builder
		for _, p := range order {
			b.AddContainer(p, p, nil)
		}
		return b.Render()
	}
	a := mk([]string{"z", "a", "m"})
	b := mk([]string{"a", "m", "z"})
	if a != b {
		t.Errorf("container order is non-deterministic:\nA:\n%s\nB:\n%s", a, b)
	}
}

func TestBuilder_LabelEscapingInRender(t *testing.T) {
	var b Builder
	b.AddNode("x", `He said "hi"`, "", nil)
	out := b.Render()
	mustContain(t, out, `x: "He said \"hi\""`)
}

func TestStyleValueQuoting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", `""`},
		{"true", "true"},
		{"false", "false"},
		{"#fde68a", `"#fde68a"`},
		{"bold", "bold"},
		{"some words", `"some words"`},
		{"plain-value", "plain-value"},
	}
	for _, c := range cases {
		got := styleValue(c.in)
		if got != c.want {
			t.Errorf("styleValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func mustContain(t *testing.T, hay, needle string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Errorf("output missing %q\n--- output ---\n%s", needle, hay)
	}
}
