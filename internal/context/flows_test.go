package context

import (
	"testing"
)

func TestBuildFlowPath_SkipsShallowPaths(t *testing.T) {
	// Flow with only 1 node should return empty
	callGraph := map[string][]string{}
	symbolNames := map[string]string{"a": "main"}

	result := buildFlowPath("a", "main", "", callGraph, symbolNames, 5)
	if result != "" {
		t.Errorf("expected empty flow for leaf node, got %q", result)
	}
}

func TestBuildFlowPath_TracesCallChain(t *testing.T) {
	callGraph := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}
	symbolNames := map[string]string{
		"a": "main",
		"b": "Execute",
		"c": "Store.Open",
	}

	result := buildFlowPath("a", "main", "", callGraph, symbolNames, 5)
	want := "main -> Execute -> Store.Open"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}

func TestBuildFlowPath_PrefersExportedOverInternal(t *testing.T) {
	// When two callees exist, prefer the exported cross-package call
	callGraph := map[string][]string{
		"a": {"b", "c"},
	}
	symbolNames := map[string]string{
		"a": "Execute",
		"b": "isKnownSubcommandOrFlag",
		"c": "Store.Open",
	}

	result := buildFlowPath("a", "Execute", "", callGraph, symbolNames, 5)
	// Should prefer Store.Open over isKnownSubcommandOrFlag
	want := "Execute -> Store.Open"
	if result != want {
		t.Errorf("got %q, want %q", result, want)
	}
}
