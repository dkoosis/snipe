package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

// Batch status constants.
const (
	batchStatusFailed    = "failed"
	batchStatusCancelled = "cancelled"
	batchStatusCompleted = "completed"
	// batchStatusCreating is a local-only breadcrumb persisted before CreateBatch returns,
	// so a crash in the CreateBatch→SaveState window leaves a recoverable trail (input_file_id)
	// instead of an orphan batch billing silently.
	batchStatusCreating = "creating"
)

var (
	embedWait     bool
	embedPollSecs int
)

// EmbedStatusResult is the output for embed-status command.
//
// Total, Completed, Failed, and Stale carry meaning at their zero values
// ("0 failed", "checked, not stale") — the very signals embed-status reports —
// so they emit unconditionally (D4). The remaining string fields stay
// omitempty: their zero values are absence, not signal. CreatedAt is a
// *time.Time so it is genuinely omitted when absent — omitempty is a no-op on
// a time.Time value, which would otherwise leak "0001-01-01T00:00:00Z" noise
// (e.g. the no_batch result, which never sets it) into the envelope (D4).
type EmbedStatusResult struct {
	BatchID    string     `json:"batch_id,omitempty"`
	Status     string     `json:"status"`
	Total      int        `json:"total"`
	Completed  int        `json:"completed"`
	Failed     int        `json:"failed"`
	Model      string     `json:"model,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	Age        string     `json:"age,omitempty"`
	Stale      bool       `json:"stale"`
	EmbedCount int        `json:"embed_count,omitempty"`
	Message    string     `json:"message,omitempty"`
}

func runEmbedStatus() error {
	// Determine directory
	dir := "."
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	snipeDir := filepath.Join(absDir, ".snipe")
	dbPath := store.DefaultIndexPath(absDir)

	// Setup output writer
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	// Load batch client
	client, err := embed.NewBatchClient(snipeDir)
	if err != nil {
		return fmt.Errorf("create batch client: %w", err)
	}

	// Load state
	state, err := client.LoadState()
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	if state == nil {
		result := EmbedStatusResult{
			Status:  "no_batch",
			Message: "No batch embedding job found. Run 'snipe index' to start one.",
		}
		return emitEmbedStatus(w, result)
	}

	// Poll loop (or single check)
	for {
		// Get current status from Voyage
		batchStatus, err := client.GetBatchStatus(GetContext(), state.BatchID)
		if err != nil {
			return fmt.Errorf("get batch status: %w", err)
		}

		// Update local state
		state.Status = batchStatus.Status
		state.Completed = batchStatus.RequestCounts.Completed
		state.Failed = batchStatus.RequestCounts.Failed
		state.OutputFileID = batchStatus.OutputFileID
		state.ErrorFileID = batchStatus.ErrorFileID
		state.UpdatedAt = time.Now()

		if err := client.SaveState(state); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save state: %v\n", err)
		}

		fmt.Fprintf(os.Stderr, "Batch %s: status=%s, completed=%d/%d, failed=%d\n",
			state.BatchID, state.Status, state.Completed, state.Total, state.Failed)

		// Handle completion
		if state.Status == batchStatusCompleted {
			// embed-status is a standalone command with no parent store handle,
			// so opening here is safe (no concurrent writer on the same file).
			s, openErr := store.Open(dbPath)
			if openErr != nil {
				return fmt.Errorf("open store: %w", openErr)
			}
			embedCount, err := downloadAndSaveEmbeddings(GetContext(), client, state, s)
			closeErr := s.Close()
			if err != nil {
				// Save failed (locked rows, parse error) — keep batch_state.json
				// so the paid batch stays recoverable. Do NOT ClearState.
				return fmt.Errorf("download embeddings: %w", err)
			}
			if closeErr != nil {
				return fmt.Errorf("close store: %w", closeErr)
			}

			// Genuine full save — safe to clear batch state.
			if err := client.ClearState(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clear state: %v\n", err)
			}

			result := EmbedStatusResult{
				BatchID:    state.BatchID,
				Status:     batchStatusCompleted,
				Total:      state.Total,
				Completed:  state.Completed,
				Failed:     state.Failed,
				Model:      state.Model,
				CreatedAt:  &state.CreatedAt,
				EmbedCount: embedCount,
				Message:    fmt.Sprintf("Downloaded and saved %d embeddings", embedCount),
			}
			return emitEmbedStatus(w, result)
		}

		// Handle failure
		if state.Status == batchStatusFailed || state.Status == batchStatusCancelled {
			if err := client.ClearState(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clear state: %v\n", err)
			}

			result := EmbedStatusResult{
				BatchID:   state.BatchID,
				Status:    state.Status,
				Total:     state.Total,
				Completed: state.Completed,
				Failed:    state.Failed,
				Model:     state.Model,
				CreatedAt: &state.CreatedAt,
				Message:   "Batch job failed or was cancelled",
			}
			return emitEmbedStatus(w, result)
		}

		// If not waiting, return current status
		if !embedWait {
			age := time.Since(state.CreatedAt)
			isStale := age > batchStaleThreshold
			msg := "Batch job in progress"
			if isStale {
				msg = fmt.Sprintf("Batch job appears stale (no progress in %v). Run 'snipe index' to auto-recover.", age.Round(time.Hour))
			}
			result := EmbedStatusResult{
				BatchID:   state.BatchID,
				Status:    state.Status,
				Total:     state.Total,
				Completed: state.Completed,
				Failed:    state.Failed,
				Model:     state.Model,
				CreatedAt: &state.CreatedAt,
				Age:       age.Round(time.Minute).String(),
				Stale:     isStale,
				Message:   msg,
			}
			return emitEmbedStatus(w, result)
		}

		// Wait and poll again — honor ctx so Ctrl+C returns promptly instead of
		// wedging for the full poll interval.
		select {
		case <-GetContext().Done():
			return GetContext().Err()
		case <-time.After(time.Duration(embedPollSecs) * time.Second):
		}
	}
}

// emitEmbedStatus renders one embed-status result. Default (Claude) surface is
// a terse one-liner; --format json emits the full envelope (D1).
func emitEmbedStatus(w *output.Writer, result EmbedStatusResult) error {
	resp := output.Response[EmbedStatusResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []EmbedStatusResult{result},
		Meta:     output.Meta{Command: cmdNameEmbedStatus},
	}
	if GetOutputFormat() == output.OutputJSON {
		return w.WriteResponse(resp)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "embeddings · %s", result.Status)
	if result.Total > 0 {
		fmt.Fprintf(&b, " · %d/%d", result.Completed, result.Total)
	}
	if result.Failed > 0 {
		fmt.Fprintf(&b, " · %d failed", result.Failed)
	}
	if result.EmbedCount > 0 {
		fmt.Fprintf(&b, " · %d saved", result.EmbedCount)
	}
	if result.Model != "" {
		fmt.Fprintf(&b, " · %s", result.Model)
	}
	if result.Age != "" {
		fmt.Fprintf(&b, " · age %s", result.Age)
	}
	if result.Stale {
		b.WriteString(" · STALE")
	}
	if result.Message != "" {
		fmt.Fprintf(&b, "\n%s", result.Message)
	}
	b.WriteByte('\n')
	_, err := os.Stdout.WriteString(b.String())
	return err
}

// embeddingSaver is the persistence seam downloadAndSaveEmbeddings writes
// through. *store.Store satisfies it; tests inject a failing implementation.
// Threading the parent's already-open *store.Store (rather than opening a
// second handle on the same SQLite file) keeps a SINGLE single-conn pool, so
// writes serialize correctly instead of contending on busy_timeout and
// returning "database is locked".
type embeddingSaver interface {
	SaveEmbedding(symbolID string, embedding []float32, model string) error
}

// embeddingStreamParser matches *embed.BatchClient.ParseBatchResults: it reads
// JSONL from r and invokes fn for each parsed embedding. Factoring it out as a
// function value lets persistEmbeddings be unit-tested without a real
// BatchClient or network round-trip.
type embeddingStreamParser func(r io.Reader, fn embed.EmbeddingHandler) error

// downloadAndSaveEmbeddings streams batch results directly to the store.
// Downloads and parses line-by-line to avoid buffering the entire payload in RAM.
//
// Per-row save failures are accumulated, NOT swallowed: if any row fails to
// persist, the function returns a non-nil error so the caller keeps
// batch_state.json and the paid batch stays recoverable. Returns the number of
// rows actually saved alongside that error.
func downloadAndSaveEmbeddings(ctx context.Context, client *embed.BatchClient, state *embed.BatchState, saver embeddingSaver) (int, error) {
	if state.OutputFileID == "" {
		return 0, fmt.Errorf("no output file available")
	}

	fmt.Fprintf(os.Stderr, "Downloading results from file %s...\n", state.OutputFileID)

	body, err := client.DownloadFile(ctx, state.OutputFileID)
	if err != nil {
		return 0, fmt.Errorf("download output file: %w", err)
	}
	defer body.Close()

	return persistEmbeddings(body, client.ParseBatchResults, saver, state.Model)
}

// persistEmbeddings streams parsed embeddings into saver, accumulating per-row
// save failures instead of swallowing them. A "database is locked" on any row
// means a paid embedding never landed; the resulting non-nil error tells the
// caller to PRESERVE batch_state.json so the batch stays recoverable rather
// than clearing it and silently dropping the paid rows (snipe-apz).
func persistEmbeddings(body io.Reader, parse embeddingStreamParser, saver embeddingSaver, model string) (int, error) {
	count := 0
	failed := 0
	var firstSaveErr error
	if err := parse(body, func(symbolID string, embedding []float32) error {
		if err := saver.SaveEmbedding(symbolID, embedding, model); err != nil {
			// Do NOT swallow: record the failure and keep streaming so we save
			// what we can, then surface a non-nil error after the stream.
			fmt.Fprintf(os.Stderr, "Warning: failed to save embedding for %s: %v\n", symbolID, err)
			failed++
			if firstSaveErr == nil {
				firstSaveErr = err
			}
			return nil // keep going; the accumulated error is returned after the stream
		}
		count++
		return nil
	}); err != nil {
		return count, fmt.Errorf("parse results: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Saved %d embeddings to index\n", count)

	if failed > 0 {
		return count, fmt.Errorf("failed to save %d of %d embeddings (first error: %w); keeping batch state for recovery", failed, failed+count, firstSaveErr)
	}

	return count, nil
}
