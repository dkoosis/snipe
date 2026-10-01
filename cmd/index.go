package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

// Embedding mode constants.
const (
	embedModeAuto     = "auto"
	embedModeBatch    = "batch"
	embedModeRealtime = "realtime"
	embedModeOff      = "off"
)

var (
	withEmbed   bool   // Legacy flag, kept for compatibility
	embedMode   string // New flag: auto, batch, realtime, off
	forceIndex  bool   // Force full re-index even if no changes detected
	skipMetrics bool   // Skip graph metrics computation (PageRank, etc.)
)

func runIndex(args []string) error {
	start := time.Now()

	// Determine directory to index
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	// Canonicalize an explicit path arg (sn-za8p): query commands resolve
	// root from os.Getwd(), which macOS returns already symlink-resolved.
	// FindProjectRoot canonicalizes too, but only when a .git/go.mod marker
	// is found; this covers the fallback case below where none is found and
	// absDir is used as-is.
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}

	// Always index relative to project root — not CWD or an arbitrary subdir (D3)
	if root := util.FindProjectRoot(absDir); root != "" {
		absDir = root
	}

	// Setup output writer
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	// Acquire lock to signal indexing in progress
	dbPath := store.DefaultIndexPath(absDir)
	if err := store.AcquireLock(dbPath); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer store.ReleaseLock(dbPath) // Always release on exit

	// Compute fingerprint
	fp, err := index.ComputeFingerprint(absDir, Version)
	if err != nil {
		return fmt.Errorf("compute fingerprint: %w", err)
	}

	// Open or create store
	s, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	// Persist repo_root before any WriteIndex call. Both full and incremental
	// writers read it to compute file_path_rel; if stale or empty, paths get
	// relativized against the wrong base (or stored absolute), breaking output.
	if err := s.SetMeta("repo_root", absDir); err != nil {
		return fmt.Errorf("store repo root: %w", err)
	}
	// module_path makes DetectModulePath authoritative on repos with no root
	// package (everything under internal/ or cmd/), where the pkg_path
	// heuristic finds nothing.
	if data, err := os.ReadFile(filepath.Join(absDir, "go.mod")); err == nil {
		if mp := modfile.ModulePath(data); mp != "" {
			_ = s.SetMeta("module_path", mp)
		}
	}

	// Change detection fast-path: skip expensive work if nothing changed
	var detection *changeDetection
	if !forceIndex {
		var detectErr error
		detection, detectErr = trySkipIndex(s, fp, absDir, start, w)
		if detectErr != nil {
			// Detection failed — fall through to full index
			fmt.Fprintf(os.Stderr, "Change detection failed: %v (proceeding with full index)\n", detectErr)
			detection = &changeDetection{result: skipResultProceedFull}
		}
		// Backfill guard: an index built before the current file-level metrics
		// existed carries a stale (or absent) version marker. A skip or
		// incremental run would never populate file_churn / the "files" graph,
		// leaving `snipe hotspots` and `metrics --kind=churn` empty until the
		// user discovers --force. Force a one-time full reindex to backfill;
		// the marker is written at the end of the metrics block, so subsequent
		// runs skip normally.
		if detection.result != skipResultProceedFull && !skipMetrics && graphmetrics.NeedBackfill(s) {
			fmt.Fprintf(os.Stderr, "Index predates current risk metrics — running full reindex to backfill (one-time)\n")
			detection = &changeDetection{result: skipResultProceedFull}
		}
		if detection.result == skipResultSkipped {
			return nil
		}
	} else {
		detection = &changeDetection{result: skipResultProceedFull}
	}

	// Delete-only fast path: skip expensive go/packages load when only files were removed
	if detection.result == skipResultProceedIncremental &&
		detection.changes != nil &&
		len(detection.changes.Modified) == 0 &&
		len(detection.changes.Added) == 0 &&
		len(detection.changes.Deleted) > 0 {
		return runDeleteOnlyIndex(s, detection.changes, absDir, start, w)
	}

	// Load packages (needed for symbol extraction)
	fmt.Fprintf(os.Stderr, "Loading packages from %s...\n", absDir)
	loadStart := time.Now()

	patterns, err := index.WorkspacePatterns(absDir)
	if err != nil {
		return fmt.Errorf("resolve workspace patterns: %w", err)
	}

	result, err := index.Load(index.LoadConfig{
		Context:  GetContext(),
		Dir:      absDir,
		Patterns: patterns,
		Tests:    true,
	})
	if err != nil {
		return fmt.Errorf("load packages: %w", err)
	}

	loadMs := time.Since(loadStart).Milliseconds()
	fmt.Fprintf(os.Stderr, "Loaded %d packages in %dms\n", len(result.Packages), loadMs)

	// Report load errors (suppress test package noise from go/packages).
	// go/packages generates spurious "no required module" warnings for
	// _test and .test packages when Tests=true.
	for _, e := range result.Errors {
		msg := e.Error()
		if strings.Contains(msg, "no required module provides package") &&
			(strings.Contains(msg, "_test") || strings.Contains(msg, ".test")) {
			continue
		}
		fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
	}

	// Extract ALL symbols (cheap, needed for position index in both paths)
	fmt.Fprintf(os.Stderr, "Extracting symbols...\n")
	symbols, err := index.ExtractSymbols(result)
	if err != nil {
		return fmt.Errorf("extract symbols: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d symbols\n", len(symbols))

	// Extract package-level doc comments
	pkgDocs := index.ExtractPackageDocs(result)
	fmt.Fprintf(os.Stderr, "Found %d package docs\n", len(pkgDocs))

	// Branch: incremental vs full
	if detection.result == skipResultProceedIncremental {
		return runIncrementalIndex(s, result, symbols, pkgDocs, detection.changes, absDir, start, w)
	}

	// Full reindex path
	// Extract refs with file caching for performance
	fmt.Fprintf(os.Stderr, "Extracting references...\n")
	fileCache := util.NewFileCache(util.DefaultMaxCachedFiles)
	refs, err := index.ExtractRefsWithCache(result, symbols, fileCache)
	if err != nil {
		return fmt.Errorf("extract refs: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d references (cached %d files)\n", len(refs), fileCache.Size())

	// Extract call graph
	fmt.Fprintf(os.Stderr, "Building call graph...\n")
	edges, err := index.ExtractCallGraph(result, symbols)
	if err != nil {
		return fmt.Errorf("extract call graph: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d call edges\n", len(edges))

	// Extract imports
	fmt.Fprintf(os.Stderr, "Extracting imports...\n")
	imports, err := index.ExtractImports(result)
	if err != nil {
		return fmt.Errorf("extract imports: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d imports\n", len(imports))

	// Extract file info (for content hashes)
	fmt.Fprintf(os.Stderr, "Computing file hashes...\n")
	files, err := index.ExtractFileInfo(absDir)
	if err != nil {
		return fmt.Errorf("extract file info: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Hashed %d files\n", len(files))

	// Write to store
	fmt.Fprintf(os.Stderr, "Writing index...\n")
	if err := s.WriteIndex(symbols, refs, edges); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Write package docs
	if err := s.WritePackageDocs(pkgDocs); err != nil {
		return fmt.Errorf("write package docs: %w", err)
	}

	// Write imports
	if err := s.WriteImports(imports); err != nil {
		return fmt.Errorf("write imports: %w", err)
	}

	// Compute graph metrics on full reindex. Incremental indexing skips
	// this — recomputed only on full reindex.
	if !skipMetrics {
		graphmetrics.Compute(s, symbols, os.Stderr)
	}

	// Extract and write string literals (env var calls + named consts)
	literals := index.ExtractLiterals(result, symbols)
	fmt.Fprintf(os.Stderr, "Found %d string literals\n", len(literals))
	if err := s.WriteLiterals(literals, absDir); err != nil {
		return fmt.Errorf("write literals: %w", err)
	}

	// Write file hashes
	if err := s.WriteFiles(files); err != nil {
		return fmt.Errorf("write files: %w", err)
	}

	// Write file→package fallback rows (sn-dzbj): tier-2 (loader) covers every
	// file go/packages actually loaded, including symbol-less ones (doc.go);
	// tier-3 (header) covers files go/packages never loaded at all
	// (build-tag-excluded), via the bare package name captured in
	// ExtractFileInfo. Loader MUST write first — tier-3's INSERT OR IGNORE
	// relies on it to implement "same-dir loader row wins" (write.go doc).
	filePkgs := index.ExtractFilePackages(result)
	if err := s.WriteFilePackages(filePkgs, absDir, store.FilePackageSourceLoader); err != nil {
		return fmt.Errorf("write file packages (loader): %w", err)
	}
	headerPkgs := make([]index.FilePackage, 0, len(files))
	for _, f := range files {
		if f.PackageName == "" {
			continue
		}
		headerPkgs = append(headerPkgs, index.FilePackage{Path: f.Path, PkgPath: f.PackageName})
	}
	if err := s.WriteFilePackages(headerPkgs, absDir, store.FilePackageSourceHeader); err != nil {
		return fmt.Errorf("write file packages (header): %w", err)
	}

	// Store fingerprint and metadata
	if err := s.SetMeta("fingerprint", fp.Combined); err != nil {
		return fmt.Errorf("store fingerprint: %w", err)
	}
	if err := s.SetMeta("indexed_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store timestamp: %w", err)
	}
	// repo_root is set earlier (before WriteIndex) so file_path_rel is computed correctly.
	// Reset incremental counter on full reindex
	_ = s.SetMeta("incremental_count", "0")
	_ = s.SetMeta("orphaned_refs", "0")

	// Determine effective embedding mode
	effectiveMode := resolveEmbedMode(embedMode, withEmbed, s)

	// Generate embeddings based on mode. Each symbol is embedded with its
	// package narrative and caller names, not just its signature (sn-6wv).
	// The context is built per case so embedModeOff pays nothing for it.
	var embedCount int
	var embedStatus string
	switch effectiveMode {
	case embedModeOff:
		embedStatus = "disabled"
	case embedModeBatch:
		ec := buildEmbedContext(symbols, edges, pkgDocs)
		status, err := startBatchEmbeddings(GetContext(), s, absDir, symbols, ec, fp.Combined)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: batch embedding failed: %v\n", err)
			embedStatus = batchStatusFailed
		} else {
			embedStatus = status
		}
	case embedModeRealtime:
		ec := buildEmbedContext(symbols, edges, pkgDocs)
		count, err := generateEmbeddings(GetContext(), s, symbols, ec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: embedding generation failed: %v\n", err)
			embedStatus = batchStatusFailed
		} else {
			embedCount = count
			embedStatus = "completed"
		}
	}

	if embedCount > 0 {
		fmt.Fprintf(os.Stderr, "Generated %d embeddings\n", embedCount)
	} else if embedStatus == "batch_started" {
		fmt.Fprintf(os.Stderr, "Batch embedding started (async). Use 'snipe embed-status' to check progress.\n")
	}

	return emitIndex(w, indexResponse(absDir, start, len(symbols), nil))
}

// generateEmbeddings creates embeddings for symbols with signatures.
func generateEmbeddings(ctx context.Context, s *store.Store, symbols []index.Symbol, ec embedContext) (int, error) {
	client, err := embed.NewClient()
	if err != nil {
		return 0, err
	}

	fmt.Fprintf(os.Stderr, "Generating embeddings with %s...\n", client.Model())

	toEmbed := filterEmbeddableSymbols(symbols, ec)
	if len(toEmbed) == 0 {
		return 0, nil
	}

	// Batch embeddings (Voyage AI supports up to 128 texts per request)
	const batchSize = 64
	total := 0

	for i := 0; i < len(toEmbed); i += batchSize {
		end := i + batchSize
		if end > len(toEmbed) {
			end = len(toEmbed)
		}
		batch := toEmbed[i:end]

		// Build texts for embedding
		texts := make([]string, len(batch))
		for j, sym := range batch {
			texts[j] = sym.Text
		}

		// Generate embeddings
		embeddings, err := client.Embed(ctx, texts, "document")
		if err != nil {
			return total, fmt.Errorf("embed batch %d: %w", i/batchSize, err)
		}

		// Store embeddings
		for j, emb := range embeddings {
			if emb == nil {
				continue
			}
			if err := s.SaveEmbedding(batch[j].ID, emb, client.Model()); err != nil {
				return total, fmt.Errorf("save embedding for %s: %w", batch[j].ID, err)
			}
			total++
		}

		fmt.Fprintf(os.Stderr, "  Embedded %d/%d symbols\n", end, len(toEmbed))
	}

	return total, nil
}

// defaultWithEmbed reports the legacy --embed default: embeddings are on when
// API credentials are available. Mirrors the cobra-era dynamic flag default.
func defaultWithEmbed() bool { return credentialsProbe() }

// credentialsProbe is a seam over embed.HasCredentials so tests can count
// probe calls. The probe may exec /usr/bin/security (10s timeout on a locked
// keychain), so --embed-mode=off paths must never reach it — heal's 15s
// backstop leaves no headroom.
var credentialsProbe = embed.HasCredentials

// resolveEmbedMode determines the effective embedding mode.
func resolveEmbedMode(mode string, legacyEmbed bool, s *store.Store) string {
	// --embed-mode=off must do ZERO keychain execs: short-circuit before any
	// credentials probe.
	if mode == embedModeOff {
		return embedModeOff
	}

	// Handle legacy --embed=false
	if !legacyEmbed && mode == embedModeAuto {
		return embedModeOff
	}

	// Check if credentials are available
	if !credentialsProbe() {
		return embedModeOff
	}

	switch mode {
	case embedModeBatch:
		return embedModeBatch
	case embedModeRealtime:
		return embedModeRealtime
	case embedModeAuto:
		// Auto: use batch for initial indexing, realtime for incremental
		count, err := s.CountEmbeddings()
		if err != nil || count == 0 {
			// No existing embeddings - use batch for initial indexing
			return embedModeBatch
		}
		// Has embeddings - use realtime for incremental updates
		return embedModeRealtime
	default:
		return embedModeAuto
	}
}

// batchStaleThreshold is how long a batch can be in validating/in_progress before considered stale.
const batchStaleThreshold = 12 * time.Hour

// recoverCompletedBatch downloads and saves the results of an already-completed
// batch, reusing the caller's open store handle so the write goes through ONE
// connection pool rather than contending on the SQLite WAL lock (snipe-apz).
//
// Returns:
//   - (false, nil): the batch is for a different index generation; state has
//     been cleared and the caller should start a fresh batch.
//   - (true, nil):  results were saved and batch state cleared.
//   - (true, err):  the save failed; batch_state.json is PRESERVED so the paid
//     batch stays recoverable. The caller must NOT start a new batch (that would
//     re-bill and orphan the completed one) — surface the error instead.
func recoverCompletedBatch(ctx context.Context, client *embed.BatchClient, state *embed.BatchState, s *store.Store, fingerprint string) (bool, error) {
	if !state.MatchesFingerprint(fingerprint) {
		fmt.Fprintf(os.Stderr, "  Batch was created for a different index version, discarding stale results...\n")
		if clearErr := client.ClearState(); clearErr != nil {
			return false, fmt.Errorf("clear stale batch state: %w", clearErr)
		}
		return false, nil
	}

	// Batch completed but results never processed — auto-recover.
	fmt.Fprintf(os.Stderr, "  Batch completed, recovering results...\n")
	count, dlErr := downloadAndSaveEmbeddings(ctx, client, state, s)
	if dlErr != nil {
		// A save error (locked rows) or a download/parse error (expired output,
		// API error) leaves batch_state.json in place so the paid batch stays
		// recoverable next run. Do NOT ClearState, do NOT start a fresh batch.
		fmt.Fprintf(os.Stderr, "  Recovery failed: %v\n", dlErr)
		fmt.Fprintf(os.Stderr, "  Keeping batch state for recovery; not starting a new batch.\n")
		return true, fmt.Errorf("recover batch embeddings (%d saved): %w", count, dlErr)
	}

	// Genuine full save — safe to clear batch state.
	fmt.Fprintf(os.Stderr, "  Recovered %d embeddings from completed batch\n", count)
	if clearErr := client.ClearState(); clearErr != nil {
		fmt.Fprintf(os.Stderr, "  Warning: failed to clear state: %v\n", clearErr)
	}
	return true, nil
}

// startBatchEmbeddings initiates async batch embedding via Voyage API.
// fingerprint identifies the index generation that these embeddings belong to.
// s is the caller's already-open store; the recovery branch reuses it so a
// recovered batch writes through ONE connection pool instead of opening a
// second handle that contends on the SQLite WAL lock (snipe-apz).
func startBatchEmbeddings(ctx context.Context, s *store.Store, repoRoot string, symbols []index.Symbol, ec embedContext, fingerprint string) (string, error) {
	snipeDir := filepath.Join(repoRoot, ".snipe")
	client, err := embed.NewBatchClient(snipeDir)
	if err != nil {
		return "", err
	}

	// Check for existing batch in progress
	state, err := client.LoadState()
	if err != nil {
		return "", fmt.Errorf("load state: %w", err)
	}

	// "creating" breadcrumb means a previous run crashed between CreateBatch and SaveState.
	// We can't auto-reconcile without a list-batches API, so warn loudly with the input_file_id
	// so the user can verify in the Voyage dashboard before re-running and double-billing.
	if state != nil && state.Status == batchStatusCreating {
		fmt.Fprintf(os.Stderr, "WARNING: previous batch creation may have leaked an orphan job.\n")
		fmt.Fprintf(os.Stderr, "  input_file_id: %s\n", state.InputFileID)
		fmt.Fprintf(os.Stderr, "  Check https://dash.voyageai.com/ for a batch tied to this file before re-running.\n")
		fmt.Fprintf(os.Stderr, "  Clearing breadcrumb and starting fresh...\n")
		if clearErr := client.ClearState(); clearErr != nil {
			return "", fmt.Errorf("clear creating-state breadcrumb: %w", clearErr)
		}
		state = nil
	}

	// A persisted "completed" status means a prior `snipe embed-status` (or index
	// recovery) fetched a finished batch but failed to download/save its results
	// and preserved the state for recovery (snipe-apz). Re-attempt the save here
	// instead of falling through to start a fresh, double-billed batch that would
	// also orphan the paid completed one. (The validating/in_progress branch below
	// only discovers a completed batch via the API; a state file already marked
	// completed would otherwise slip past it unrecovered.)
	if state != nil && state.Status == batchStatusCompleted {
		handled, rErr := recoverCompletedBatch(ctx, client, state, s, fingerprint)
		if rErr != nil {
			return "", rErr
		}
		if handled {
			return "batch_recovered", nil
		}
		// Fingerprint mismatch: state cleared, fall through to start a new batch.
		state = nil
	}

	if state != nil && (state.Status == "validating" || state.Status == "in_progress") {
		// Check if batch is stale (stuck for too long)
		age := time.Since(state.UpdatedAt)
		switch {
		case age > batchStaleThreshold:
			// Try to verify actual status from Voyage API
			fmt.Fprintf(os.Stderr, "Batch %s has been %q for %v, checking actual status...\n",
				state.BatchID, state.Status, age.Round(time.Minute))

			actualStatus, err := client.GetBatchStatus(ctx, state.BatchID)
			if err != nil {
				// Can't reach API or batch doesn't exist - clear stale state
				fmt.Fprintf(os.Stderr, "  Could not verify batch status: %v\n", err)
				fmt.Fprintf(os.Stderr, "  Clearing stale batch state and starting fresh...\n")
				if clearErr := client.ClearState(); clearErr != nil {
					return "", fmt.Errorf("clear stale state: %w", clearErr)
				}
				// Fall through to start new batch
			} else {
				switch actualStatus.Status {
				case batchStatusFailed, batchStatusCancelled, "expired":
					// Batch is dead, clear state
					fmt.Fprintf(os.Stderr, "  Batch is %s, clearing state and starting fresh...\n", actualStatus.Status)
					if clearErr := client.ClearState(); clearErr != nil {
						return "", fmt.Errorf("clear dead batch state: %w", clearErr)
					}
					// Fall through to start new batch
				case batchStatusCompleted:
					// Update state with output file info from API, then recover.
					state.Status = batchStatusCompleted
					state.OutputFileID = actualStatus.OutputFileID
					state.ErrorFileID = actualStatus.ErrorFileID
					state.Completed = actualStatus.RequestCounts.Completed
					state.Failed = actualStatus.RequestCounts.Failed
					state.UpdatedAt = time.Now()

					handled, rErr := recoverCompletedBatch(ctx, client, state, s, fingerprint)
					if rErr != nil {
						return "", rErr
					}
					if handled {
						return "batch_recovered", nil
					}
					// Fingerprint mismatch: state cleared, fall through to new batch.
				default:
					// Batch is still running according to API, but very old
					fmt.Fprintf(os.Stderr, "  Batch is still %q according to Voyage AI.\n", actualStatus.Status)
					fmt.Fprintf(os.Stderr, "  Run 'snipe embed-status --wait' to monitor, or 'snipe index --embed-mode=off' to skip.\n")
					return "batch_in_progress", nil
				}
			}
		case !state.MatchesFingerprint(fingerprint):
			// Batch is for a different index version — discard and start fresh
			fmt.Fprintf(os.Stderr, "Batch %s is for a different index version, discarding...\n", state.BatchID)
			if clearErr := client.ClearState(); clearErr != nil {
				return "", fmt.Errorf("clear mismatched batch state: %w", clearErr)
			}
			// Fall through to start new batch
		default:
			fmt.Fprintf(os.Stderr, "Batch embedding already in progress (batch_id: %s, status: %s, age: %v)\n",
				state.BatchID, state.Status, age.Round(time.Minute))
			return "batch_in_progress", nil
		}
	}

	// Filter symbols worth embedding
	toEmbed := filterEmbeddableSymbols(symbols, ec)
	if len(toEmbed) == 0 {
		return "no_symbols", nil
	}

	fmt.Fprintf(os.Stderr, "Starting batch embedding for %d symbols with %s...\n", len(toEmbed), client.Model())

	// Write JSONL file
	jsonlPath, err := client.WriteJSONL(toEmbed, snipeDir)
	if err != nil {
		return "", fmt.Errorf("write JSONL: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Wrote %s\n", jsonlPath)

	// Upload file
	fmt.Fprintf(os.Stderr, "  Uploading to Voyage AI...\n")
	fileResp, err := client.UploadFile(ctx, jsonlPath)
	if err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Uploaded file_id: %s\n", fileResp.ID)

	// Persist a "creating" breadcrumb BEFORE CreateBatch so a crash in the
	// CreateBatch→SaveState window leaves the input_file_id on disk for recovery.
	now := time.Now()
	breadcrumb := &embed.BatchState{
		InputFileID:      fileResp.ID,
		Status:           batchStatusCreating,
		Total:            len(toEmbed),
		CreatedAt:        now,
		UpdatedAt:        now,
		Model:            client.Model(),
		IndexFingerprint: fingerprint,
	}
	if err := client.SaveState(breadcrumb); err != nil {
		return "", fmt.Errorf("save creating-state breadcrumb: %w", err)
	}

	// Create batch
	fmt.Fprintf(os.Stderr, "  Creating batch job...\n")
	batchResp, err := client.CreateBatch(ctx, fileResp.ID)
	if err != nil {
		// CreateBatch failed: no server-side batch was created, so no billing risk.
		// Drop the breadcrumb so the next run starts cleanly.
		if clearErr := client.ClearState(); clearErr != nil {
			fmt.Fprintf(os.Stderr, "  Warning: failed to clear creating-state breadcrumb: %v\n", clearErr)
		}
		return "", fmt.Errorf("create batch: %w", err)
	}
	// Log batch_id to stderr IMMEDIATELY so a SaveState crash still leaves a recovery trail.
	fmt.Fprintf(os.Stderr, "  Created batch_id: %s (status: %s)\n", batchResp.ID, batchResp.Status)

	// Save state for polling
	newState := &embed.BatchState{
		BatchID:          batchResp.ID,
		InputFileID:      fileResp.ID,
		Status:           batchResp.Status,
		Total:            len(toEmbed),
		Completed:        0,
		Failed:           0,
		CreatedAt:        now,
		UpdatedAt:        time.Now(),
		Model:            client.Model(),
		IndexFingerprint: fingerprint,
	}
	if err := client.SaveState(newState); err != nil {
		return "", fmt.Errorf("save state: %w", err)
	}

	// Clean up local JSONL file
	_ = os.Remove(jsonlPath) // G104: best-effort cleanup of temporary file

	return "batch_started", nil
}

// runIncrementalIndex performs an incremental index update for changed files only.
func runIncrementalIndex(s *store.Store, result *index.LoadResult, allSymbols []index.Symbol, pkgDocs []index.PackageDoc, changes *index.ChangeResult, absDir string, start time.Time, w *output.Writer) error {
	// Build file filter set (modified + added files only)
	changedFiles := make([]string, 0, len(changes.Modified)+len(changes.Added))
	changedFiles = append(changedFiles, changes.Modified...)
	changedFiles = append(changedFiles, changes.Added...)

	onlyFiles := make(map[string]bool, len(changedFiles))
	for _, f := range changedFiles {
		onlyFiles[f] = true
	}

	// Filter symbols: only those from changed files
	var changedSymbols []index.Symbol
	for i := range allSymbols {
		sym := &allSymbols[i]
		if onlyFiles[sym.FilePath] {
			changedSymbols = append(changedSymbols, *sym)
		}
	}

	embedBefore, refreshEmbed := beginEmbedRefresh(s)

	// Extract refs ONLY for changed files (main savings)
	fmt.Fprintf(os.Stderr, "Extracting references for %d changed files...\n", len(changedFiles))
	fileCache := util.NewFileCache(util.DefaultMaxCachedFiles)
	refs, err := index.ExtractRefsFiltered(result, allSymbols, fileCache, onlyFiles)
	if err != nil {
		return fmt.Errorf("extract refs: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d references\n", len(refs))

	// Extract call edges ONLY for changed files
	fmt.Fprintf(os.Stderr, "Building call graph for changed files...\n")
	edges, err := index.ExtractCallGraphFiltered(result, allSymbols, onlyFiles)
	if err != nil {
		return fmt.Errorf("extract call graph: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d call edges\n", len(edges))

	// Extract imports ONLY for changed files
	fmt.Fprintf(os.Stderr, "Extracting imports for changed files...\n")
	imports, err := index.ExtractImportsFiltered(result, onlyFiles)
	if err != nil {
		return fmt.Errorf("extract imports: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d imports\n", len(imports))

	// Extract string literals for changed files only — written atomically
	// inside WriteIndexIncremental's tx so symbols + literal-refs for a
	// changed file commit in the same generation (snipe-er5).
	literals := index.ExtractLiteralsFiltered(result, allSymbols, onlyFiles)
	fmt.Fprintf(os.Stderr, "Found %d string literals in changed files\n", len(literals))

	// Write incremental update (symbols, refs, edges, imports, literals)
	fmt.Fprintf(os.Stderr, "Writing incremental index...\n")
	incResult, err := s.WriteIndexIncremental(changedSymbols, refs, edges, imports, literals, changedFiles, changes.Deleted)
	if err != nil {
		return fmt.Errorf("write incremental index: %w", err)
	}

	// Write package docs (full replace — cheap and ensures consistency)
	if err := s.WritePackageDocs(pkgDocs); err != nil {
		return fmt.Errorf("write package docs: %w", err)
	}

	// Update file hashes for ALL files (cheap stat calls)
	fmt.Fprintf(os.Stderr, "Computing file hashes...\n")
	files, err := index.ExtractFileInfo(absDir)
	if err != nil {
		return fmt.Errorf("extract file info: %w", err)
	}
	if err := s.WriteFiles(files); err != nil {
		return fmt.Errorf("write files: %w", err)
	}

	// Write file→package fallback rows (sn-dzbj) — same two-tier write as the
	// full-reindex path (see comment there). `result`/`files` cover ALL files
	// here too (ExtractFileInfo/the go/packages load are not filtered to
	// changed files), so this stays correct on every incremental run, not just
	// full reindexes.
	filePkgs := index.ExtractFilePackages(result)
	if err := s.WriteFilePackages(filePkgs, absDir, store.FilePackageSourceLoader); err != nil {
		return fmt.Errorf("write file packages (loader): %w", err)
	}
	headerPkgs := make([]index.FilePackage, 0, len(files))
	for _, f := range files {
		if f.PackageName == "" {
			continue
		}
		headerPkgs = append(headerPkgs, index.FilePackage{Path: f.Path, PkgPath: f.PackageName})
	}
	if err := s.WriteFilePackages(headerPkgs, absDir, store.FilePackageSourceHeader); err != nil {
		return fmt.Errorf("write file packages (header): %w", err)
	}

	// Update metadata
	if err := s.SetMeta("indexed_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store timestamp: %w", err)
	}

	if refreshEmbed {
		finishEmbedRefresh(s, embedBefore, changedSymbols)
	}

	// Build summary
	nMod := len(changes.Modified)
	nAdd := len(changes.Added)
	nDel := len(changes.Deleted)
	fmt.Fprintf(os.Stderr, "Incremental: updated %d files (%d modified, %d added, %d deleted)\n",
		nMod+nAdd+nDel, nMod, nAdd, nDel)

	symCount, _, _, _ := s.GetStats()
	return emitIndex(w, indexResponse(absDir, start, symCount, orphanSuggestion(incResult)))
}

// runDeleteOnlyIndex handles the case where only files were deleted.
// Skips the expensive go/packages load entirely — just removes dead rows from the DB.
func runDeleteOnlyIndex(s *store.Store, changes *index.ChangeResult, absDir string, start time.Time, w *output.Writer) error {
	nDel := len(changes.Deleted)
	fmt.Fprintf(os.Stderr, "Delete-only: removing %d files (skipping package load)\n", nDel)

	// A deleted file can hold the only caller of a symbol elsewhere; that
	// callee's embedded text ("called by: …") goes stale (sn-1ewy).
	embedBefore, refreshEmbed := beginEmbedRefresh(s)

	// Remove symbols, refs, edges, imports, string_refs, embeddings, purposes,
	// and file entries for deleted files — all within a single transaction in
	// WriteIndexIncremental (nil literals + deleted files prunes string_refs).
	incResult, err := s.WriteIndexIncremental(nil, nil, nil, nil, nil, nil, changes.Deleted)
	if err != nil {
		return fmt.Errorf("delete-only incremental: %w", err)
	}

	// Update metadata
	if err := s.SetMeta("indexed_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store timestamp: %w", err)
	}

	if refreshEmbed {
		finishEmbedRefresh(s, embedBefore, nil)
	}

	fmt.Fprintf(os.Stderr, "Deleted %d files\n", nDel)

	symCount, _, _, _ := s.GetStats()
	return emitIndex(w, indexResponse(absDir, start, symCount, orphanSuggestion(incResult)))
}

// indexResponse builds a standard index command response.
// emitIndex renders the index response. Default (Claude) surface is a terse
// one-liner (progress detail already went to stderr); --format json emits the
// full envelope (D1).
func emitIndex(w *output.Writer, resp protocol.Response[any]) error {
	if GetOutputFormat() == output.OutputJSON {
		return w.WriteResponse(resp)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "indexed · %d symbols · %s · %dms\n",
		resp.Meta.Total, resp.Meta.IndexState, resp.Meta.Ms)
	for _, s := range resp.Suggestions {
		fmt.Fprintf(&b, "→ %s", s.Command)
		if s.Description != "" {
			fmt.Fprintf(&b, "  (%s)", s.Description)
		}
		b.WriteByte('\n')
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}

func indexResponse(absDir string, start time.Time, symCount int, suggestions []protocol.Suggestion) protocol.Response[any] {
	return protocol.Response[any]{
		Protocol:    protocol.ProtocolVersion,
		Ok:          true,
		Suggestions: suggestions,
		Meta: protocol.Meta{
			Command:    cmdNameIndex,
			RepoRoot:   absDir,
			IndexState: protocol.IndexFresh,
			Ms:         time.Since(start).Milliseconds(),
			Total:      symCount,
		},
	}
}

// orphanSuggestion returns a suggestion to rebuild if orphaned refs exist.
func orphanSuggestion(inc *store.IncrementalResult) []protocol.Suggestion {
	if inc == nil || inc.OrphanedRefs == 0 {
		return nil
	}
	return []protocol.Suggestion{{
		Command:     "snipe index --force",
		Description: fmt.Sprintf("Full rebuild to clear %d orphaned refs", inc.OrphanedRefs),
		Priority:    3,
		Condition:   "incremental_orphans",
	}}
}

// skipResult describes the outcome of change detection.
type skipResult int

const (
	skipResultProceedFull        skipResult = iota // full reindex needed
	skipResultSkipped                              // no changes, skip entirely
	skipResultProceedIncremental                   // incremental update possible
)

// changeDetection holds the result of change detection for use by runIndex.
type changeDetection struct {
	result  skipResult
	changes *index.ChangeResult
}

// trySkipIndex checks whether the index is already up-to-date and can be skipped.
// Returns changeDetection describing what action to take.
func trySkipIndex(s *store.Store, fp *index.Fingerprint, absDir string, start time.Time, w *output.Writer) (*changeDetection, error) {
	storedFP, fpErr := s.GetMeta("fingerprint")
	if fpErr != nil {
		return &changeDetection{result: skipResultProceedFull}, nil //nolint:nilerr // No stored fingerprint means first index — proceed with full build
	}

	if storedFP != fp.Combined {
		fmt.Fprintf(os.Stderr, "Build config changed, full re-index required\n")
		return &changeDetection{result: skipResultProceedFull}, nil
	}

	// Fingerprint matches — check source file changes
	storedFiles, filesErr := s.GetAllFiles()
	if filesErr != nil || len(storedFiles) == 0 {
		return &changeDetection{result: skipResultProceedFull}, nil //nolint:nilerr // No stored file data means fall through to full index
	}

	changes, detectErr := index.DetectChanges(absDir, storedFiles, index.DefaultExclude())
	if detectErr != nil {
		return nil, detectErr
	}

	if !changes.HasChanges {
		// No changes — skip indexing
		fmt.Fprintf(os.Stderr, "Index up to date: %s\n", changes.Summary())
		symCount, _, _, _ := s.GetStats()
		resp := protocol.Response[any]{
			Protocol: protocol.ProtocolVersion,
			Ok:       true,
			Results:  nil,
			Meta: protocol.Meta{
				Command:    cmdNameIndex,
				RepoRoot:   absDir,
				IndexState: protocol.IndexFresh,
				Ms:         time.Since(start).Milliseconds(),
				Total:      symCount,
			},
		}
		return &changeDetection{result: skipResultSkipped}, emitIndex(w, resp)
	}

	// Changes detected — decide between incremental and full reindex
	totalFiles := changes.TotalChanged() + changes.Unchanged
	if totalFiles > 0 && changes.TotalChanged()*100/totalFiles > 50 {
		// >50% files changed — full reindex is more efficient
		fmt.Fprintf(os.Stderr, "Changes detected (%s), >50%% files changed — full re-index\n", changes.Summary())
		return &changeDetection{result: skipResultProceedFull}, nil
	}

	fmt.Fprintf(os.Stderr, "Changes detected: %s\n", changes.Summary())
	return &changeDetection{result: skipResultProceedIncremental, changes: changes}, nil
}
