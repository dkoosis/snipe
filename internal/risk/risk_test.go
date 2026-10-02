package risk

import "testing"

func intp(n int) *int { return &n }

func TestCurve_HalfAtMidpoint(t *testing.T) {
	t.Parallel()
	if got := curve(20, 20); got != 0.5 {
		t.Fatalf("curve(20, 20) = %v, want 0.5", got)
	}
	if got := curve(0, 20); got != 0 {
		t.Fatalf("curve(0, 20) = %v, want 0", got)
	}
	if got := curve(1000, 20); got <= 0.95 || got >= 1 {
		t.Fatalf("curve(1000, 20) = %v, want just under 1", got)
	}
}

func TestScore_IsTheWeightedMeanOfTheFactors(t *testing.T) {
	t.Parallel()
	m := Measures{
		Reach:      Reach{Callers: midCallers, Importers: midImporters}, // 0.5, 0.5 → 0.5
		Difficulty: Difficulty{CycloMax: intp(midCyclo), CognitiveMax: intp(midCognitive)},
		History:    &History{BugCommits: midBugCommits, Churn: midChurn},
		Kind:       Kind{RiskFlags: map[string]int{"concurrency": midRiskFlags}, Roles: map[string]int{"persistence": midRoles}, Exported: midExported},
		Tests:      Tests{ChangedFuncs: 2, Untested: []string{"p.a"}},
	}
	m.score()
	for name, v := range m.Factors {
		if v != 0.5 {
			t.Fatalf("factor %s = %v, want 0.5 (every measure at its midpoint)", name, v)
		}
	}
	if len(m.Factors) != 5 || m.Score == nil || *m.Score != 0.5 {
		t.Fatalf("score = %v over %d factors, want 0.5 over 5", m.Score, len(m.Factors))
	}
	if m.WeightsVersion != weightsVersion || len(m.Weights) != 5 {
		t.Fatalf("weights not carried: v%d %v", m.WeightsVersion, m.Weights)
	}
}

func TestScore_LeavesOutAFactorWithNoSource(t *testing.T) {
	t.Parallel()
	// No changed functions, no churn: difficulty, history and safety net are
	// missing, not zero, and the score is the mean of reach and kind alone.
	m := Measures{
		Reach: Reach{Callers: midCallers, Importers: midImporters},
		Kind:  Kind{RiskFlags: map[string]int{"concurrency": midRiskFlags}, Roles: map[string]int{"api_boundary": midRoles}, Exported: midExported},
	}
	m.score()
	for _, f := range []string{FactorDifficulty, FactorHistory, FactorSafetyNet} {
		if _, ok := m.Factors[f]; ok {
			t.Fatalf("factor %s present with no source: %v", f, m.Factors)
		}
	}
	if m.Score == nil || *m.Score != 0.5 {
		t.Fatalf("score = %v, want 0.5 from reach and kind", m.Score)
	}
}

func TestScore_InternalRoleIsNotKindRisk(t *testing.T) {
	t.Parallel()
	m := Measures{Kind: Kind{Roles: map[string]int{"internal": 40}}}
	m.score()
	if m.Factors[FactorKind] != 0 {
		t.Fatalf("kind = %v for internal-only symbols, want 0", m.Factors[FactorKind])
	}
}

func TestDegrade_HasNoScore(t *testing.T) {
	t.Parallel()
	m := Degrade(ChangeStats{Files: 3}, "index is stale")
	if m.Score != nil || !m.Degraded || m.Changed.Files != 3 {
		t.Fatalf("degraded = %+v, want no score, degraded, size kept", m)
	}
}
