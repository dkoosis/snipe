package output

import (
	"fmt"
	"strings"
	"testing"
)

func TestLifecycleCallerChain(t *testing.T) {
	tests := []struct {
		name    string
		callers []LifecycleCallerNode
		want    string
	}{
		{"empty", nil, ""},
		{"single", []LifecycleCallerNode{{Name: "foo", Depth: 1}}, "foo"},
		{"two hops", []LifecycleCallerNode{
			{Name: "bar", Depth: 2},
			{Name: "foo", Depth: 1},
		}, "foo ← bar"},
		{"stable sort by name at same depth", []LifecycleCallerNode{
			{Name: "zzz", Depth: 1},
			{Name: "aaa", Depth: 1},
		}, "aaa ← zzz"},
		{"tests folded into count", []LifecycleCallerNode{
			{Name: "TestA", Depth: 1},
			{Name: "TestB", Depth: 1},
			{Name: "BenchmarkC", Depth: 1},
			{Name: "realCaller", Depth: 1},
		}, "realCaller ← +3 tests"},
		{"non-test cap with +N more", []LifecycleCallerNode{
			{Name: "a", Depth: 1}, {Name: "b", Depth: 1}, {Name: "c", Depth: 1},
			{Name: "d", Depth: 1}, {Name: "e", Depth: 1}, {Name: "f", Depth: 1},
			{Name: "g", Depth: 1}, {Name: "h", Depth: 1}, {Name: "i", Depth: 1},
			{Name: "j", Depth: 1},
		}, "a ← b ← c ← d ← e ← f ← g ← h ← +2 more"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lifecycleCallerChain(tc.callers)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteClaudeLifecycle_CallersRendered(t *testing.T) {
	w := NewWriter(&strings.Builder{}, false, OutputClaude)
	var b strings.Builder
	results := []LifecycleResult{
		{
			Type:         "Nug",
			TypeFile:     "store/nug.go",
			TypeLine:     10,
			TypeKind:     "struct",
			TotalRefs:    3,
			FunctionRefs: 2,
			Groups: []LifecycleGroup{
				{
					Role:  "Create",
					Count: 1,
					Funcs: []LifecycleFunction{
						{
							Name:   "NewNug",
							File:   "store/nug.go",
							Line:   20,
							Signal: "rule:struct-literal",
							Callers: []LifecycleCallerNode{
								{Name: "handleCreate", Depth: 1},
								{Name: "router", Depth: 2},
							},
						},
					},
				},
				{
					Role:  "Read",
					Count: 0,
					Funcs: nil,
				},
			},
		},
	}
	w.writeClaudeLifecycle(&b, results, Meta{})
	out := b.String()

	if !strings.Contains(out, "# Lifecycle: Nug") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "## Create (1)") {
		t.Error("missing Create group")
	}
	if !strings.Contains(out, "## Read (0)") {
		t.Error("standard CRUD bucket should render even when empty (snipe-0sb)")
	}
	if !strings.Contains(out, "signal: rule:struct-literal") {
		t.Error("missing signal")
	}
	if !strings.Contains(out, "callers: handleCreate ← router") {
		t.Errorf("missing caller chain, got:\n%s", out)
	}
	// ID should not appear in terse mode (dropped for D4)
	if strings.Contains(out, "[") && strings.Contains(out, "]") && strings.Contains(out, "hex") {
		t.Error("hex ID leaked into Claude text output")
	}
}

func TestWriteClaudeLifecycle_SummaryFormat(t *testing.T) {
	w := NewWriter(&strings.Builder{}, false, OutputClaude)
	var b strings.Builder
	funcs := make([]LifecycleFunction, 0, 15)
	for i := 0; i < 15; i++ {
		funcs = append(funcs, LifecycleFunction{
			Name:   fmt.Sprintf("fn%d", i),
			File:   "x.go",
			Line:   i,
			Signal: "should-not-render",
			Callers: []LifecycleCallerNode{
				{Name: "shouldNotRender", Depth: 1},
			},
		})
	}
	results := []LifecycleResult{
		{
			Type:         "Big",
			TypeFile:     "x.go",
			TotalRefs:    15,
			FunctionRefs: 15,
			Summary:      true,
			Groups: []LifecycleGroup{
				{Role: "Read", Count: 15, Funcs: funcs},
			},
		},
	}
	w.writeClaudeLifecycle(&b, results, Meta{})
	out := b.String()

	if strings.Contains(out, "should-not-render") || strings.Contains(out, "shouldNotRender") {
		t.Error("summary leaked signal/callers")
	}
	if !strings.Contains(out, "## Read (15)") {
		t.Error("summary missing group header")
	}
	if !strings.Contains(out, "+3 more") {
		t.Errorf("summary missing inline cap suffix, got:\n%s", out)
	}
	// Should be tight: well under 50 lines.
	if got := strings.Count(out, "\n"); got > 10 {
		t.Errorf("summary too verbose: %d lines\n%s", got, out)
	}
}

func TestTruncateLifecycleToTokenBudget(t *testing.T) {
	makeResult := func(fnCount int, withCallers bool) LifecycleResult {
		funcs := make([]LifecycleFunction, fnCount)
		for i := range funcs {
			fn := LifecycleFunction{
				Name:   "Func",
				File:   "store/nug.go",
				Line:   i + 1,
				Signal: "rule:name",
			}
			if withCallers {
				fn.Callers = []LifecycleCallerNode{
					{Name: "callerA", Depth: 1},
					{Name: "callerB", Depth: 2},
				}
			}
			funcs[i] = fn
		}
		return LifecycleResult{
			Type:         "Nug",
			TotalRefs:    fnCount,
			FunctionRefs: fnCount,
			Groups: []LifecycleGroup{
				{Role: "Create", Count: fnCount, Funcs: funcs},
			},
		}
	}

	t.Run("zero budget = no-op", func(t *testing.T) {
		r := makeResult(5, true)
		out, truncated := TruncateLifecycleToTokenBudget(r, 0)
		if truncated {
			t.Error("expected no truncation with budget=0")
		}
		if len(out.Groups[0].Funcs) != 5 {
			t.Error("expected all funcs preserved")
		}
	})

	t.Run("fits = no-op", func(t *testing.T) {
		r := makeResult(1, false)
		_, truncated := TruncateLifecycleToTokenBudget(r, 10000)
		if truncated {
			t.Error("expected no truncation when result fits")
		}
	})

	t.Run("callers dropped first", func(t *testing.T) {
		r := makeResult(3, true)
		estimateFull := lifecycleTokenEstimate(r)
		rNoCallers := makeResult(3, false)
		estimateNoCallers := lifecycleTokenEstimate(rNoCallers)

		// Budget: fits without callers, doesn't fit with callers.
		budget := (estimateFull + estimateNoCallers) / 2
		out, truncated := TruncateLifecycleToTokenBudget(r, budget)
		if !truncated {
			t.Error("expected truncation")
		}
		// All callers should be gone, funcs preserved.
		for _, f := range out.Groups[0].Funcs {
			if len(f.Callers) > 0 {
				t.Error("callers should have been stripped in pass 1")
			}
		}
		if len(out.Groups[0].Funcs) != 3 {
			t.Errorf("funcs should be preserved in pass 1, got %d", len(out.Groups[0].Funcs))
		}
	})

	t.Run("funcs trimmed when callers not enough", func(t *testing.T) {
		r := makeResult(20, false)
		_, truncated := TruncateLifecycleToTokenBudget(r, 5)
		if !truncated {
			t.Error("expected truncation with tiny budget")
		}
	})

	t.Run("stable ordering preserved", func(t *testing.T) {
		r := makeResult(5, false)
		out1, _ := TruncateLifecycleToTokenBudget(r, 10)
		out2, _ := TruncateLifecycleToTokenBudget(r, 10)
		if len(out1.Groups[0].Funcs) != len(out2.Groups[0].Funcs) {
			t.Error("truncation is not deterministic")
		}
	})
}

func TestWriteClaudeLifecycle_JSONEnvelope(t *testing.T) {
	var buf strings.Builder
	w := NewWriter(&buf, false, OutputJSON)
	results := []LifecycleResult{
		{
			Type:         "Store",
			TotalRefs:    1,
			FunctionRefs: 1,
			Groups: []LifecycleGroup{
				{Role: "Create", Count: 1, Funcs: []LifecycleFunction{
					{Name: "NewStore", File: "store.go", Line: 5,
						Callers: []LifecycleCallerNode{{Name: "main", Depth: 1}}},
				}},
			},
		},
	}
	resp := Response[LifecycleResult]{
		Protocol: ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta:     Meta{Command: "lifecycle"},
	}
	if err := w.WriteResponse(resp); err != nil {
		t.Fatalf("WriteResponse error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"protocol"`) {
		t.Error("JSON envelope missing protocol field")
	}
	if !strings.Contains(out, `"callers"`) {
		t.Error("JSON envelope missing callers field")
	}
	if !strings.Contains(out, `"role"`) {
		t.Error("JSON envelope missing role field")
	}
}
