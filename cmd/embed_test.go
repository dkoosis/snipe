package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbedStatusResultEmitsMeaningfulZeros guards that Total, Completed,
// Failed, and Stale serialize at their zero values. "0 failed" and
// "checked, not stale" (stale=false) are the signals embed-status reports;
// omitempty silently dropped them from the JSON envelope (D4, snipe-0xt).
func TestEmbedStatusResultEmitsMeaningfulZeros(t *testing.T) {
	data, err := json.Marshal(EmbedStatusResult{Status: "completed"})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	for _, field := range []string{"total", "completed", "failed", "stale"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("embed-status JSON missing %q at zero value — omitempty dropped a meaningful zero (snipe-0xt)", field)
		}
	}
	if raw["stale"] != false {
		t.Errorf("stale = %v, want false", raw["stale"])
	}
	// CreatedAt is a *time.Time so an absent timestamp is genuinely omitted —
	// a time.Time would have leaked "0001-01-01T00:00:00Z" past omitempty,
	// the D4 noise the no_batch result exhibits (snipe-0xt).
	if _, ok := raw["created_at"]; ok {
		t.Errorf("embed-status JSON has created_at at zero value — *time.Time should omit it (snipe-0xt)")
	}
}

// failingSaver fails every SaveEmbedding, simulating a "database is locked"
// stall on the SQLite WAL — the failure mode that previously got swallowed and
// caused paid Voyage embeddings to vanish (snipe-apz).
type failingSaver struct {
	calls int
}

func (f *failingSaver) SaveEmbedding(symbolID string, embedding []float32, model string) error {
	f.calls++
	return errors.New("database is locked")
}

// countingSaver records every successful save. Used for the happy path.
type countingSaver struct {
	saved int
}

func (c *countingSaver) SaveEmbedding(symbolID string, embedding []float32, model string) error {
	c.saved++
	return nil
}

// fakeParser returns an embeddingStreamParser that emits the given symbol IDs
// (with a trivial embedding each), ignoring the reader entirely — so the test
// needs no network, no real BatchClient, and no credentials.
func fakeParser(symbolIDs ...string) embeddingStreamParser {
	return func(_ io.Reader, fn func(symbolID string, embedding []float32) error) error {
		for _, id := range symbolIDs {
			if err := fn(id, []float32{0.1, 0.2}); err != nil {
				return err
			}
		}
		return nil
	}
}

// TestPersistEmbeddings_FailingSaverPropagatesError is the core regression for
// snipe-apz: when SaveEmbedding fails, the error must NOT be swallowed — it has
// to propagate so the caller keeps batch_state.json and the paid batch stays
// recoverable.
func TestPersistEmbeddings_FailingSaverPropagatesError(t *testing.T) {
	saver := &failingSaver{}

	count, err := persistEmbeddings(strings.NewReader(""), fakeParser("symA", "symB"), saver, "voyage-test")

	if err == nil {
		t.Fatal("persistEmbeddings returned nil error despite every SaveEmbedding failing — the lock error was swallowed (snipe-apz regression)")
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (no row persisted when every save fails)", count)
	}
	if saver.calls != 2 {
		t.Errorf("SaveEmbedding called %d times, want 2 (stream should keep going to save what it can)", saver.calls)
	}
}

// TestPersistEmbeddings_SuccessReturnsNoError asserts the happy path still
// returns a nil error (so the caller is free to ClearState only here).
func TestPersistEmbeddings_SuccessReturnsNoError(t *testing.T) {
	saver := &countingSaver{}

	count, err := persistEmbeddings(strings.NewReader(""), fakeParser("symA", "symB", "symC"), saver, "voyage-test")

	if err != nil {
		t.Fatalf("persistEmbeddings returned error on all-success path: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

// TestRecoveryGuard_PreservesBatchStateOnSaveFailure models the caller's
// ClearState guard end to end: a save failure must leave batch_state.json on
// disk so the paid batch can be recovered on the next run. Before snipe-apz the
// recovery branch swallowed the error, "succeeded", and ClearState deleted the
// only record of the paid batch.
func TestRecoveryGuard_PreservesBatchStateOnSaveFailure(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "batch_state.json")
	if err := os.WriteFile(statePath, []byte(`{"batch_id":"b-123","status":"completed"}`), 0o600); err != nil {
		t.Fatalf("seed batch_state.json: %v", err)
	}

	saver := &failingSaver{}
	_, err := persistEmbeddings(strings.NewReader(""), fakeParser("symA"), saver, "voyage-test")
	if err == nil {
		t.Fatal("expected non-nil error from failing save")
	}

	// Caller guard: ClearState runs ONLY when persistEmbeddings returns nil.
	// Here err != nil, so the (simulated) caller must skip the delete.
	clearState := func() error { return os.Remove(statePath) }
	if err == nil {
		_ = clearState()
	}

	if _, statErr := os.Stat(statePath); statErr != nil {
		t.Fatalf("batch_state.json was removed despite a save failure — paid batch became unrecoverable (snipe-apz): %v", statErr)
	}
}

// TestRecoveryGuard_ClearsBatchStateOnSuccess is the complement: a genuinely
// complete save returns nil, so the caller is free to clear the state file.
func TestRecoveryGuard_ClearsBatchStateOnSuccess(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "batch_state.json")
	if err := os.WriteFile(statePath, []byte(`{"batch_id":"b-123","status":"completed"}`), 0o600); err != nil {
		t.Fatalf("seed batch_state.json: %v", err)
	}

	saver := &countingSaver{}
	_, err := persistEmbeddings(strings.NewReader(""), fakeParser("symA"), saver, "voyage-test")
	if err != nil {
		t.Fatalf("unexpected error on success path: %v", err)
	}

	clearState := func() error { return os.Remove(statePath) }
	if err == nil {
		if cErr := clearState(); cErr != nil {
			t.Fatalf("clear state: %v", cErr)
		}
	}

	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("batch_state.json should be cleared on full success, stat err = %v", statErr)
	}
}
