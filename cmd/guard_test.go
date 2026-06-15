package cmd

import (
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

func TestGuardAllowed(t *testing.T) {
	allow := []string{"agent/cost_budget.go", "supervisor/reload.go"}
	cases := map[string]bool{
		"modules/agent/cost_budget.go":       true,
		"modules/agent/supervisor/reload.go": true,
		"modules/agent/agent.go":             false,
		"":                                   false,
	}
	for file, want := range cases {
		if got := allowed(allow, file); got != want {
			t.Errorf("allowed(%q) = %v, want %v", file, got, want)
		}
	}
	if allowed(nil, "any.go") {
		t.Error("empty allow-list should exempt nothing")
	}
}

func TestGuardKindSet(t *testing.T) {
	if kindSet(nil) != nil {
		t.Error("empty kinds must return nil (means: all kinds)")
	}
	m := kindSet([]string{"func", " method "})
	if !m["func"] || !m["method"] {
		t.Errorf("kindSet trimmed/parsed wrong: %v", m)
	}
	if m["type"] {
		t.Error("type should not be in the set")
	}
}

func TestRefViolations(t *testing.T) {
	ref := func(kind string, files ...string) query.BoundaryRef {
		locs := make([]query.BoundaryLoc, len(files))
		for i, f := range files {
			locs[i] = query.BoundaryLoc{File: f, Line: i + 1}
		}
		return query.BoundaryRef{Symbol: "New", Kind: kind, SourcePkg: "a", TargetPkg: "b", Locations: locs}
	}

	t.Run("test files excluded by default", func(t *testing.T) {
		r := guardRule{Name: "x"}
		got := refViolations(r, ref("func", "prod.go", "prod_test.go"))
		if len(got) != 1 || got[0].File != "prod.go" {
			t.Fatalf("expected only prod.go, got %+v", got)
		}
	})

	t.Run("include_tests keeps test sites", func(t *testing.T) {
		r := guardRule{Name: "x", IncludeTests: true}
		if got := refViolations(r, ref("func", "prod.go", "prod_test.go")); len(got) != 2 {
			t.Fatalf("expected 2 with include_tests, got %d", len(got))
		}
	})

	t.Run("allow-list grandfathers a site", func(t *testing.T) {
		r := guardRule{Name: "x", Allow: []string{"reload.go"}}
		got := refViolations(r, ref("func", "agent/reload.go", "agent/other.go"))
		if len(got) != 1 || got[0].File != "agent/other.go" {
			t.Fatalf("allow-list should drop reload.go, got %+v", got)
		}
	})

	t.Run("no locations still yields one violation", func(t *testing.T) {
		if got := refViolations(guardRule{Name: "x"}, ref("func")); len(got) != 1 || got[0].File != "" {
			t.Fatalf("fileless crossing must not be hidden, got %+v", got)
		}
	})
}
