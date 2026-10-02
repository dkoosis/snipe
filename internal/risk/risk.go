package risk

// Measures is snipe's answer for a diff: raw measures of the changed code and
// its history. No score, weights, thresholds or verdict: what the numbers mean
// for review is the caller's call, made on evidence snipe does not have.
type Measures struct {
	Changed     ChangeStats    `json:"changed"`
	Callers     int            `json:"callers"`      // call sites of the changed symbols, capped at callerCap
	CallerFiles int            `json:"caller_files"` // files those call sites are in
	Importers   int            `json:"importers"`    // packages importing a changed package (the most for any one)
	Commits     int            `json:"commits"`      // commits to a changed Go file in the churn window (the most for any one)
	Roles       map[string]int `json:"roles"`        // changed symbols per architectural role
	RiskFlags   map[string]int `json:"risk_flags"`   // changed symbols per risk flag
	Degraded    bool           `json:"degraded"`
	Note        string         `json:"note,omitempty"`
}

// ChangeStats is the diff's shape. Symbols are production symbols only; a
// _test.go edit adds files, not symbols.
type ChangeStats struct {
	Files   int `json:"files"`
	GoFiles int `json:"go_files"`
	Symbols int `json:"symbols"`
}

// Degrade is a result that could not be measured: why says what stopped it.
// changed keeps the diff shape when git could read it.
func Degrade(changed ChangeStats, why string) Measures {
	return Measures{Changed: changed, Roles: map[string]int{}, RiskFlags: map[string]int{}, Degraded: true, Note: why}
}
