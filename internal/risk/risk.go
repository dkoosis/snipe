package risk

import "sort"

// Verdict levels — repo-agnostic risk. The cc-plugins review-judge maps these to
// its none|codex|full CI tier; snipe never learns the tier names.
const (
	VerdictLow    = "low"
	VerdictMedium = "medium"
	VerdictHigh   = "high"
)

// Score thresholds for the verdict level (tunable; documented in the plan).
// Deduped signal weights sum to the score.
const (
	scoreHigh   = 5 // score >= scoreHigh   → high
	scoreMedium = 2 // score >= scoreMedium → medium; else low
)

// Signal is one risk reason: a stable code, a human-legible detail, and a weight.
type Signal struct {
	Signal string `json:"signal"`
	Detail string `json:"detail"`
	Weight int    `json:"weight"`
}

// ChangeStats describes the diff's shape (observability, not scored).
type ChangeStats struct {
	Files   int `json:"files"`
	GoFiles int `json:"go_files"`
	Symbols int `json:"symbols"`
}

// Verdict is snipe's risk answer for a diff — the canonical contract consumers read.
type Verdict struct {
	Verdict  string      `json:"verdict"`
	Score    int         `json:"score"`
	Reasons  []Signal    `json:"reasons"`
	Changed  ChangeStats `json:"changed"`
	Degraded bool        `json:"degraded"`
	Note     string      `json:"note,omitempty"`
}

// Fuse reduces raw signals to a verdict. It dedups by signal code (each class fires
// at most once, keeping its highest-weight instance), sums the deduped weights, and
// thresholds to a level. A degraded assessment is forced to low — absent signal is
// never evidence of high risk. Reasons come out deterministically ordered.
func Fuse(signals []Signal, changed ChangeStats, degraded bool, note string) Verdict {
	best := make(map[string]Signal, len(signals))
	for _, s := range signals {
		if cur, ok := best[s.Signal]; !ok || s.Weight > cur.Weight {
			best[s.Signal] = s
		}
	}

	reasons := make([]Signal, 0, len(best))
	score := 0
	for _, s := range best {
		reasons = append(reasons, s)
		score += s.Weight
	}
	// Weight desc, then signal code asc — stable for JSON/golden output.
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Weight != reasons[j].Weight {
			return reasons[i].Weight > reasons[j].Weight
		}
		return reasons[i].Signal < reasons[j].Signal
	})

	return Verdict{
		Verdict:  level(score, degraded),
		Score:    score,
		Reasons:  reasons,
		Changed:  changed,
		Degraded: degraded,
		Note:     note,
	}
}

// level maps a score to a verdict, forcing low when the assessment degraded.
func level(score int, degraded bool) string {
	if degraded {
		return VerdictLow
	}
	switch {
	case score >= scoreHigh:
		return VerdictHigh
	case score >= scoreMedium:
		return VerdictMedium
	default:
		return VerdictLow
	}
}
