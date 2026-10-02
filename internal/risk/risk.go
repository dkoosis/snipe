package risk

import "math"

// Measures is snipe's answer for a diff: the measures that bear on how much
// review it needs, grouped into five factors, each scored 0-1, and one overall
// score from visible weights. No band or verdict: the caller picks its own
// cut-offs, and the weights are tuned as outcomes show which factors matter.
type Measures struct {
	Score          *float64           `json:"score"` // weighted mean of Factors; null when degraded
	Factors        map[string]float64 `json:"factors"`
	Weights        map[string]float64 `json:"weights"`
	WeightsVersion int                `json:"weights_version"`
	Changed        ChangeStats        `json:"changed"`
	Reach          Reach              `json:"reach"`
	Difficulty     Difficulty         `json:"difficulty"`
	History        *History           `json:"history"` // null when the index holds no churn
	Kind           Kind               `json:"kind"`
	Tests          Tests              `json:"tests"`
	Focus          []Focus            `json:"focus"` // changed functions to look at first
	Degraded       bool               `json:"degraded"`
	Note           string             `json:"note,omitempty"`
}

// ChangeStats is the diff's size. Symbols are production symbols only; code
// lines count non-test Go files.
type ChangeStats struct {
	Files            int `json:"files"`
	GoFiles          int `json:"go_files"`
	TestFiles        int `json:"test_files"`
	Symbols          int `json:"symbols"`
	CodeLinesAdded   int `json:"code_lines_added"`
	CodeLinesRemoved int `json:"code_lines_removed"`
}

// Reach is how far a mistake in the change could spread.
type Reach struct {
	Callers     int  `json:"callers"`      // call sites of the changed symbols, capped at callerCap
	CallerFiles int  `json:"caller_files"` // files those call sites are in
	Importers   int  `json:"importers"`    // packages importing a changed package, the most for any one
	PkgRank     *int `json:"pkg_rank"`     // PageRank rank of the most central changed package; null when not computed
	PkgCount    int  `json:"pkg_count"`    // packages ranked
}

// Difficulty is how hard the changed functions are to get right, measured on
// the functions themselves at the head of the diff.
type Difficulty struct {
	CycloMax     *int `json:"cyclo_max"`     // null when no function changed
	CognitiveMax *int `json:"cognitive_max"` // null when no function changed
}

// History is whether the changed files have hurt before.
type History struct {
	Commits    int     `json:"commits"`     // non-merge commits, the most for any changed file
	BugCommits int     `json:"bug_commits"` // commits with a Bead-Type: bug trailer, the most for any changed file
	Churn      float64 `json:"churn"`       // recency-weighted commits (180-day half-life), the most for any changed file
	Authors    int     `json:"authors"`     // distinct authors, the most for any changed file
}

// Kind is what sort of code the change touches.
type Kind struct {
	Roles     map[string]int `json:"roles"`      // changed symbols per architectural role
	RiskFlags map[string]int `json:"risk_flags"` // changed symbols per risk flag
	Exported  int            `json:"exported"`   // changed exported symbols
}

// Tests is whether anything would catch a mistake.
type Tests struct {
	ChangedFuncs int      `json:"changed_funcs"`
	Untested     []string `json:"untested"` // changed functions no test reaches
}

// Focus is one changed function worth a reviewer's attention first.
type Focus struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Callers   int    `json:"callers"` // direct callers (call-graph in-degree)
	Cyclo     int    `json:"cyclo"`
	Cognitive int    `json:"cognitive"`
	Tested    bool   `json:"tested"`
}

// Factor names.
const (
	FactorReach      = "reach"
	FactorDifficulty = "difficulty"
	FactorHistory    = "history"
	FactorKind       = "kind"
	FactorSafetyNet  = "safety_net"
)

// weightsVersion names the weights below; bump it whenever they or the curve
// midpoints change, so an old score can be read against what produced it.
const weightsVersion = 1

// weights start equal: nothing yet says one factor matters more than another.
var weights = map[string]float64{
	FactorReach:      1,
	FactorDifficulty: 1,
	FactorHistory:    1,
	FactorKind:       1,
	FactorSafetyNet:  1,
}

// Curve midpoints: the raw value that scores 0.5. Part of weightsVersion.
const (
	midCallers    = 20
	midImporters  = 5
	midCyclo      = 10
	midCognitive  = 15
	midBugCommits = 5
	midChurn      = 10
	midRiskFlags  = 1
	midRoles      = 3
	midExported   = 3
)

// curve maps a raw count to 0-1: 0 at 0, 0.5 at mid, toward 1 beyond.
func curve(x, mid float64) float64 {
	if x <= 0 {
		return 0
	}
	return x / (x + mid)
}

// round3 keeps three decimals: more would claim a precision the measures lack.
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

func mean(xs ...float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// score fills Factors, Weights and Score from the measures. A factor whose
// source is missing is left out rather than scored 0, and the overall score is
// the weighted mean of the factors present.
func (m *Measures) score() {
	m.Weights, m.WeightsVersion = weights, weightsVersion
	f := map[string]float64{}

	reach := []float64{curve(float64(m.Reach.Callers), midCallers), curve(float64(m.Reach.Importers), midImporters)}
	if m.Reach.PkgRank != nil && m.Reach.PkgCount > 0 {
		// Rank 1 of N → 1; rank N of N → 1/N.
		reach = append(reach, float64(m.Reach.PkgCount-*m.Reach.PkgRank+1)/float64(m.Reach.PkgCount))
	}
	f[FactorReach] = mean(reach...)

	if m.Difficulty.CycloMax != nil && m.Difficulty.CognitiveMax != nil {
		f[FactorDifficulty] = mean(curve(float64(*m.Difficulty.CycloMax), midCyclo),
			curve(float64(*m.Difficulty.CognitiveMax), midCognitive))
	}

	if m.History != nil {
		f[FactorHistory] = mean(curve(float64(m.History.BugCommits), midBugCommits), curve(m.History.Churn, midChurn))
	}

	flags, roles := 0, 0
	for _, n := range m.Kind.RiskFlags {
		flags += n
	}
	for r, n := range m.Kind.Roles {
		if r != "internal" {
			roles += n
		}
	}
	f[FactorKind] = mean(curve(float64(flags), midRiskFlags), curve(float64(roles), midRoles),
		curve(float64(m.Kind.Exported), midExported))

	if m.Tests.ChangedFuncs > 0 {
		f[FactorSafetyNet] = float64(len(m.Tests.Untested)) / float64(m.Tests.ChangedFuncs)
	}

	sum, wsum := 0.0, 0.0
	for name, v := range f {
		sum += weights[name] * v
		wsum += weights[name]
		f[name] = round3(v)
	}
	m.Factors = f
	if wsum > 0 {
		s := round3(sum / wsum)
		m.Score = &s
	}
}

// Degrade is a result that could not be measured: why says what stopped it.
// changed keeps the diff's size when git could read it. Score stays null.
func Degrade(changed ChangeStats, why string) Measures {
	return Measures{
		Factors: map[string]float64{}, Weights: weights, WeightsVersion: weightsVersion,
		Changed: changed,
		Kind:    Kind{Roles: map[string]int{}, RiskFlags: map[string]int{}},
		Tests:   Tests{Untested: []string{}}, Focus: []Focus{},
		Degraded: true, Note: why,
	}
}
