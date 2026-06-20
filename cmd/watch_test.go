package cmd

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingReindex is an injectable runReindex that records the files it was
// asked to index (via a side channel the test sets up before invocation) and
// the contexts it received.
type recordingReindex struct {
	mu    sync.Mutex
	calls int
	ctxs  []context.Context
	err   error
}

func (r *recordingReindex) fn(ctx context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.ctxs = append(r.ctxs, ctx)
	return r.err
}

func (r *recordingReindex) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestDrainOnCancelRunsFinalReindex covers the core snipe-l6a fix: when the
// watch loop is cancelled with files pending (and no reindex in flight), the
// drain runs one final reindex so the last edit before Ctrl+C isn't dropped.
func TestDrainOnCancelRunsFinalReindex(t *testing.T) {
	rec := &recordingReindex{}
	done := make(chan reindexResultT, 1) // empty: nothing in flight

	drainOnCancelWith("dir", false, []string{"b.go"}, done, rec.fn)

	if got := rec.count(); got != 1 {
		t.Fatalf("drain ran reindex %d times; want exactly 1 (the final drain)", got)
	}
}

// TestDrainOnCancelNoPendingSkips confirms the drain is a no-op when nothing is
// pending and no reindex is in flight — no wasted final reindex on a clean stop.
func TestDrainOnCancelNoPendingSkips(t *testing.T) {
	rec := &recordingReindex{}
	done := make(chan reindexResultT, 1)

	drainOnCancelWith("dir", false, nil, done, rec.fn)

	if got := rec.count(); got != 0 {
		t.Fatalf("drain ran reindex %d times on a clean stop; want 0", got)
	}
}

// TestDrainOnCancelWaitsForInflightThenDrains models Ctrl+C while a reindex is
// running: the in-flight subprocess was killed by the cancelled parent ctx, so
// its goroutine reports an error on reindexDone. The drain must (a) wait for
// that result rather than starting an overlapping reindex, (b) fold the killed
// reindex's files back in, and (c) run exactly one final reindex covering both
// the re-folded files and files queued during the reindex.
func TestDrainOnCancelWaitsForInflightThenDrains(t *testing.T) {
	rec := &recordingReindex{}
	done := make(chan reindexResultT, 1)

	// The killed in-flight reindex reports its files with an error (subprocess
	// killed by parent ctx cancel — its work didn't land durably).
	done <- reindexResultT{files: []string{"a.go"}, err: context.Canceled}

	start := time.Now()
	// indexing=true, plus a file queued during the reindex.
	drainOnCancelWith("dir", true, []string{"b.go"}, done, rec.fn)
	elapsed := time.Since(start)

	if got := rec.count(); got != 1 {
		t.Fatalf("drain ran reindex %d times; want exactly 1 (no overlapping reindex)", got)
	}
	// Must not block on the drain timeout — the in-flight result was ready.
	if elapsed > watchDrainTimeout {
		t.Fatalf("drain blocked %v; want prompt (in-flight result was ready)", elapsed)
	}
}

// TestDrainOnCancelInflightCleanFinishStillDrainsPending confirms that when the
// in-flight reindex finishes cleanly (no error) but files were queued during
// it, the drain still runs a final reindex for those queued files. Otherwise the
// edit made during the last reindex would be silently lost.
func TestDrainOnCancelInflightCleanFinishStillDrainsPending(t *testing.T) {
	rec := &recordingReindex{}
	done := make(chan reindexResultT, 1)

	// In-flight reindex finished cleanly before cancel landed.
	done <- reindexResultT{files: []string{"a.go"}, err: nil}

	// b.go was queued during that reindex and is still pending.
	drainOnCancelWith("dir", true, []string{"b.go"}, done, rec.fn)

	if got := rec.count(); got != 1 {
		t.Fatalf("drain ran reindex %d times; want 1 (pending b.go must still drain)", got)
	}
}

// TestDrainOnCancelInflightTimesOut guards the shutdown-can't-hang property: if
// the in-flight reindex never reports (its goroutine wedged), the drain must
// still return within roughly the bounded timeout rather than blocking forever.
func TestDrainOnCancelInflightTimesOut(t *testing.T) {
	if testing.Short() {
		t.Skip("waits ~watchDrainTimeout")
	}
	rec := &recordingReindex{}
	done := make(chan reindexResultT) // never sent on, unbuffered

	start := time.Now()
	// indexing=true but nothing will ever arrive on done.
	drainOnCancelWith("dir", true, nil, done, rec.fn)
	elapsed := time.Since(start)

	if elapsed < watchDrainTimeout {
		t.Fatalf("drain returned in %v; want it to honor the ~%v bounded wait", elapsed, watchDrainTimeout)
	}
	// No pending files after the (timed-out) wait, so no final reindex.
	if got := rec.count(); got != 0 {
		t.Fatalf("drain ran reindex %d times; want 0 (no pending after timeout)", got)
	}
}
