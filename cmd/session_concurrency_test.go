package cmd

import (
	"fmt"
	"testing"

	ctxpkg "github.com/dkoosis/snipe/internal/context"
)

// TestRecordSessionQueryConcurrent guards the session read-modify-write lock
// (sessionMu). A single process can fan out multiple recordSessionQuery
// goroutines against one session.json — e.g. `snipe pack id1 id2 id3` via
// runPackMulti. Before the lock, overlapping Load→RecordQuery→Save cycles read
// the same snapshot and the last atomic rename won, silently dropping the other
// symbols' history (CR/Codex review, PR #234). With the lock, every distinct
// symbol survives. Runs under -race to also catch the concurrent file write.
func TestRecordSessionQueryConcurrent(t *testing.T) {
	projectRoot := t.TempDir()

	const n = 20 // == maxQueries: every distinct symbol must land, none trimmed
	for i := range n {
		sym := fmt.Sprintf("Sym%02d", i)
		recordSessionQuery(projectRoot, sym, sym+".go", i+1, "func", "def")
	}

	sessionWG.Wait() // unbounded: isolate RMW correctness from the drain-timeout policy

	session, err := ctxpkg.LoadSession(projectRoot)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	got := make(map[string]bool, len(session.Queries))
	for _, q := range session.Queries {
		got[q.Symbol] = true
	}
	for i := range n {
		sym := fmt.Sprintf("Sym%02d", i)
		if !got[sym] {
			t.Errorf("lost session record for %s (have %d of %d) — RMW cycle not serialized",
				sym, len(session.Queries), n)
		}
	}
}
