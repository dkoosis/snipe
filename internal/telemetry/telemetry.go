// Package telemetry appends per-invocation usage records to
// .snipe/usage.jsonl. Local only, zero network: the file exists so "is snipe
// the first tool reached for?" is measurable (ground truth for the eval),
// not guessed from transcript archaeology. Set SNIPE_NO_TELEMETRY=1 to
// disable. Failures are silent — telemetry must never break a query.
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu     sync.Mutex
	path   string
	caller string
)

// Record is one usage event. Outcome is "ok" or the error code.
type Record struct {
	TS      string `json:"ts"`
	Command string `json:"cmd"`
	Outcome string `json:"outcome"`
	Ms      int64  `json:"ms"`
	Caller  string `json:"caller,omitempty"`
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

// Emit appends one event. No-op when unconfigured or disabled.
func Emit(command, outcome string, ms int64) {
	if os.Getenv("SNIPE_NO_TELEMETRY") != "" {
		return
	}
	mu.Lock()
	p, c := path, caller
	mu.Unlock()
	if p == "" {
		return
	}

	rec := Record{
		TS:      time.Now().Format(time.RFC3339),
		Command: command,
		Outcome: outcome,
		Ms:      ms,
		Caller:  c,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}

	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G304 -- path derived from project root
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
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
