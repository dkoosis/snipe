package context

import (
	"testing"
)

func TestCompressToCommandTable_BoilerplateDetection(t *testing.T) {
	details := []EntryPointRef{
		{Name: "runDef", Purpose: "Look up definitions", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runRefs", Purpose: "Find references", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runCallers", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runContext", Purpose: "Generate boot context", Callees: []string{"FindProjectRoot", "GenerateBoot"}},
		{Name: "main", Callees: []string{"Execute"}},
	}

	table := compressToCommandTable(details)

	if table == nil {
		t.Fatal("expected non-nil command table")
		return
	}

	// Boilerplate should contain the common callees
	boilerplateSet := make(map[string]bool)
	for _, c := range table.BoilerplatePattern {
		boilerplateSet[c] = true
	}
	if !boilerplateSet["GetOutputConfig"] || !boilerplateSet["NewWriter"] || !boilerplateSet["OpenStore"] {
		t.Errorf("boilerplate should contain common callees, got %v", table.BoilerplatePattern)
	}

	// main and Execute should be excluded
	for _, cmd := range table.Commands {
		if cmd.Name == "main" || cmd.Name == "Execute" {
			t.Errorf("trivial entry point %q should be excluded", cmd.Name)
		}
	}

	// runContext should have notable callees (non-boilerplate)
	var contextCmd *CommandEntry
	for i, cmd := range table.Commands {
		if cmd.Name == "runContext" {
			contextCmd = &table.Commands[i]
			break
		}
	}
	if contextCmd == nil {
		t.Fatal("expected runContext in commands")
		return
	}
	if len(contextCmd.Callees) == 0 {
		t.Error("runContext should have notable callees (non-boilerplate)")
	}

	// runDef should have NO callees (all boilerplate)
	for _, cmd := range table.Commands {
		if cmd.Name == "runDef" && len(cmd.Callees) > 0 {
			t.Errorf("runDef callees should be empty (all boilerplate), got %v", cmd.Callees)
		}
	}
}

func TestCompressToCommandTable_Empty(t *testing.T) {
	table := compressToCommandTable(nil)
	if table != nil {
		t.Error("expected nil for empty input")
	}
}

func TestCompressToCommandTable_OnlyTrivial(t *testing.T) {
	details := []EntryPointRef{
		{Name: "main", Callees: []string{"Execute"}},
		{Name: "Execute", Callees: []string{"rootCmd.Execute"}},
	}
	table := compressToCommandTable(details)
	if table != nil {
		t.Error("expected nil when only trivial entry points exist")
	}
}

func TestCompressToCommandTable_DeduplicatesCallees(t *testing.T) {
	details := []EntryPointRef{
		{Name: "runShow", Callees: []string{"WriteError", "WriteError", "OpenStore"}},
		{Name: "runDef", Callees: []string{"OpenStore", "WriteError"}},
		{Name: "runRefs", Callees: []string{"OpenStore", "WriteError"}},
	}
	table := compressToCommandTable(details)
	if table == nil {
		t.Fatal("expected non-nil")
		return
	}
	for _, cmd := range table.Commands {
		seen := make(map[string]bool)
		for _, c := range cmd.Callees {
			if seen[c] {
				t.Errorf("duplicate callee %q in %s", c, cmd.Name)
			}
			seen[c] = true
		}
	}
}

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

func TestPickBestCallee_AvoidsTerminals(t *testing.T) {
	callGraph := map[string][]string{
		"writeIndex_id": {"doWork_id"},
	}
	symbolNames := map[string]string{
		"db_id":         "Store.DB",
		"close_id":      "Store.Close",
		"writeIndex_id": "Store.WriteIndex",
		"doWork_id":     "doWork",
	}
	visited := map[string]bool{}

	candidates := []string{"db_id", "close_id", "writeIndex_id"}
	best := pickBestCallee(candidates, visited, symbolNames, callGraph)

	if best != "writeIndex_id" {
		got := symbolNames[best]
		t.Errorf("expected WriteIndex (non-terminal), got %q", got)
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
