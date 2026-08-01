// Package telemetry appends per-invocation usage records to
// .snipe/usage.jsonl. Local only, zero network: the file exists so "is snipe
// the first tool reached for?" is measurable (ground truth for the eval),
// not guessed from transcript archaeology. Set SNIPE_NO_TELEMETRY=1 to
// disable. Failures are silent — telemetry must never break a query.
package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	mu         sync.Mutex
	path       string
	caller     string
	sessionKey string
)

// Rung is one of the six canonical resolution-strategy buckets emitted on
// every usage.jsonl row (sn-r1do.1, D2's bare→exact→CI→method→fuzzy→semantic
// cascade). Downstream consumers (ferret, sn-r1do.2/.3) key on this exact
// vocabulary — do not introduce new values without updating this comment and
// those consumers.
const (
	RungExact           = "exact"
	RungCaseInsensitive = "caseinsensitive"
	RungMethod          = "method"
	RungFuzzy           = "fuzzy"
	RungSemantic        = "semantic"
	RungNotFound        = "notfound"
)

// ciMatchDegradedMarker mirrors output.DegradedCIMatch as a literal to avoid
// an import cycle (internal/output already imports internal/telemetry for
// Emit, so telemetry cannot import output back).
const ciMatchDegradedMarker = "ci-match"

// Record is one usage event. Outcome is "ok" or the error code.
type Record struct {
	TS             string   `json:"ts"`
	Command        string   `json:"cmd"`
	Outcome        string   `json:"outcome"`
	Ms             int64    `json:"ms"`
	Caller         string   `json:"caller,omitempty"`
	Arg            string   `json:"arg,omitempty"`
	Rung           string   `json:"rung,omitempty"`
	CandidateCount int      `json:"candidate_count,omitempty"`
	IndexState     string   `json:"index_state,omitempty"`
	SessionKey     string   `json:"session_key,omitempty"`
	TriedRungs     []string `json:"tried_rungs,omitempty"` // raw decision-path trace, verbatim
}

// Fields is the input to Emit — a struct rather than positional args because
// the row now carries eight-plus values (sn-r1do.1).
type Fields struct {
	Command        string
	Outcome        string
	Ms             int64
	Arg            string
	Rung           string
	CandidateCount int
	IndexState     string
	TriedRungs     []string
}

// SetRoot points the sink at <root>/.snipe/usage.jsonl. Called once the
// project root is known; events before that are dropped.
func SetRoot(root string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(root, ".snipe", "usage.jsonl")
}

// SetCaller tags subsequent events with a caller id (--caller flag).
func SetCaller(c string) {
	mu.Lock()
	defer mu.Unlock()
	caller = c
}

// SetSessionKey tags subsequent events with a session join-key, so an
// external miner (ferret, sn-r1do.2) can group rows from one invocation
// stream without re-deriving the key itself.
func SetSessionKey(k string) {
	mu.Lock()
	defer mu.Unlock()
	sessionKey = k
}

// ResolveSessionKey derives a session join-key: the Claude Code session id
// when running as a Claude Code subprocess (verified live env var, not a
// guess), else a synthetic cwd+time-bucket fallback for direct-terminal/CI
// use. Best-effort — not required to distinguish every standalone session
// precisely, only to avoid dropping the field entirely.
func ResolveSessionKey(root string) string {
	if v := os.Getenv("CLAUDE_CODE_SESSION_ID"); v != "" {
		return v
	}
	// 5-minute time bucket: long enough to keep one human/CI debugging burst
	// together, short enough not to smear separate sessions hours apart into
	// one key. No usage-distribution data exists yet to tune this further —
	// revisit once sn-r1do.3's mining pass has real numbers.
	return fmt.Sprintf("%s@%d", root, time.Now().Unix()/300)
}

// ClassifyRung reduces a decision-path trace (plus the degraded-marker list
// and outcome) to one of the six canonical rung tokens. Pure, total,
// panic-free: telemetry must never break a query.
//
// Any outcome other than "ok" classifies as RungNotFound — the six-value
// vocabulary does not distinguish NOT_FOUND from AMBIGUOUS_SYMBOL or other
// error codes; the row's Outcome field already carries that distinction.
//
// On the "ok" path: an empty/unrecognized trace defaults to RungExact —
// decisionPath is only populated on fallback paths today (cmd/def.go,
// cmd/search.go), so silence means the literal query was served, mirroring
// the existing DegradedCIMatch/D4 convention (silence = served).
//
// Augment-path tokens (name_augment:*, sim_augment:*) that follow an already
// -served exact/fuzzy hit are deliberately not special-cased: Rung describes
// "the deepest resolution work that ran" for this row, not strictly "what
// answered result #1." Callers needing the precise sequence read TriedRungs.
func ClassifyRung(decisionPath, degraded []string, outcome string) string {
	if outcome != "ok" {
		return RungNotFound
	}
	for _, d := range degraded {
		if d == ciMatchDegradedMarker {
			return RungCaseInsensitive
		}
	}
	for i := len(decisionPath) - 1; i >= 0; i-- {
		tok := decisionPath[i]
		switch {
		case strings.HasPrefix(tok, "sim:"), strings.HasPrefix(tok, "sim_augment:"):
			return RungSemantic
		case strings.HasPrefix(tok, "index_fallback:"), strings.HasPrefix(tok, "name_augment:"), strings.HasPrefix(tok, "rg:"):
			return RungFuzzy
		case tok == "lookup:name_select", strings.HasPrefix(tok, "lookup:file_qualified"):
			return RungMethod
		case tok == "lookup:name", tok == "lookup:id", tok == "lookup:position":
			return RungExact
		}
	}
	return RungExact
}

// Emit appends one event. No-op when unconfigured or disabled.
func Emit(f Fields) {
	if os.Getenv("SNIPE_NO_TELEMETRY") != "" {
		return
	}
	mu.Lock()
	p, c, sk := path, caller, sessionKey
	mu.Unlock()
	if p == "" {
		return
	}

	rec := Record{
		TS:             time.Now().Format(time.RFC3339),
		Command:        f.Command,
		Outcome:        f.Outcome,
		Ms:             f.Ms,
		Caller:         c,
		Arg:            f.Arg,
		Rung:           f.Rung,
		CandidateCount: f.CandidateCount,
		IndexState:     f.IndexState,
		SessionKey:     sk,
		TriedRungs:     f.TriedRungs,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}

	fh, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G304 -- path derived from project root
	if err != nil {
		return
	}
	defer fh.Close()
	_, _ = fh.Write(append(line, '\n'))
}

// ReadAll loads every record from <root>/.snipe/usage.jsonl, skipping
// malformed lines (the file is append-only across versions).
func ReadAll(root string) ([]Record, error) {
	data, err := os.ReadFile(filepath.Join(root, ".snipe", "usage.jsonl")) // #nosec G304 -- path derived from project root
	if err != nil {
		return nil, err
	}
	var out []Record
	start := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			if i > start {
				var r Record
				if json.Unmarshal(data[start:i], &r) == nil && r.Command != "" {
					out = append(out, r)
				}
			}
			start = i + 1
		}
	}
	return out, nil
}
