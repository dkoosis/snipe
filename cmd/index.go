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
	"github.com/dkoosis/snipe/internal/gitchurn"
	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
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

	// Compute graph metrics (PageRank over import graph) on full reindex.
	// Incremental indexing skips this — recomputed only on full reindex.
	if !skipMetrics {
		if err := computeImportSCCs(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: SCC computation failed: %v\n", err)
		}
		if err := computeImportPageRank(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: metrics computation failed: %v\n", err)
		}
		if err := computeImportHITS(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: HITS computation failed: %v\n", err)
		}
		if err := computeImportDegreeAndEigenvector(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: degree/eigenvector computation failed: %v\n", err)
		}
		if err := computeImportCoupling(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: coupling computation failed: %v\n", err)
		}
		if err := computeAbstractness(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: abstractness computation failed: %v\n", err)
		}
		if err := computeLCOM4(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: LCOM4 computation failed: %v\n", err)
		}
		if err := computeCycloRollups(s, symbols); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cyclo rollup computation failed: %v\n", err)
		}
		if err := computeCognitiveRollups(s, symbols); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cognitive rollup computation failed: %v\n", err)
		}
		if err := computeCallsGraphMetrics(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: calls-graph metrics computation failed: %v\n", err)
		}
		if err := computeFileCyclo(s, symbols); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: file cyclo computation failed: %v\n", err)
		}
		if err := computeChurn(s); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git churn computation failed: %v\n", err)
		}
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

	// Generate embeddings based on mode
	var embedCount int
	var embedStatus string
	switch effectiveMode {
	case embedModeOff:
		embedStatus = "disabled"
	case embedModeBatch:
		status, err := startBatchEmbeddings(GetContext(), s, absDir, symbols, fp.Combined)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: batch embedding failed: %v\n", err)
			embedStatus = batchStatusFailed
		} else {
			embedStatus = status
		}
	case embedModeRealtime:
		ec, err := generateEmbeddings(GetContext(), s, symbols)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: embedding generation failed: %v\n", err)
			embedStatus = batchStatusFailed
		} else {
			embedCount = ec
			embedStatus = "completed"
		}
	}

	if embedCount > 0 {
		fmt.Fprintf(os.Stderr, "Generated %d embeddings\n", embedCount)
	} else if embedStatus == "batch_started" {
		fmt.Fprintf(os.Stderr, "Batch embedding started (async). Use 'snipe embed-status' to check progress.\n")
	}

	return w.WriteResponse(indexResponse(absDir, start, len(symbols), nil))
}

// filterEmbeddableSymbols returns symbols suitable for embedding (functions, methods,
// types with signatures or docs) as SymbolText with combined text for the embedding model.
func filterEmbeddableSymbols(symbols []index.Symbol) []embed.SymbolText {
	var result []embed.SymbolText
	for i := range symbols {
		sym := &symbols[i]
		switch sym.Kind {
		case index.KindFunc, index.KindMethod, index.KindType, index.KindInterface, index.KindStruct:
			if sym.Signature != "" || sym.Doc != "" {
				text := sym.Name
				if sym.Signature != "" {
					text += " " + sym.Signature
				}
				if sym.Doc != "" {
					text += " " + sym.Doc
				}
				result = append(result, embed.SymbolText{
					ID:   sym.ID,
					Text: text,
				})
			}
		case index.KindVar, index.KindConst, index.KindField:
			// Skip - these typically don't have meaningful signatures for embedding
		}
	}
	return result
}

// generateEmbeddings creates embeddings for symbols with signatures.
func generateEmbeddings(ctx context.Context, s *store.Store, symbols []index.Symbol) (int, error) {
	client, err := embed.NewClient()
	if err != nil {
		return 0, err
	}

	fmt.Fprintf(os.Stderr, "Generating embeddings with %s...\n", client.Model())

	toEmbed := filterEmbeddableSymbols(symbols)
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
func defaultWithEmbed() bool { return embed.HasCredentials() }

// resolveEmbedMode determines the effective embedding mode.
func resolveEmbedMode(mode string, legacyEmbed bool, s *store.Store) string {
	// Handle legacy --embed=false
	if !legacyEmbed && mode == embedModeAuto {
		return embedModeOff
	}

	// Check if credentials are available
	if !embed.HasCredentials() {
		return embedModeOff
	}

	switch mode {
	case embedModeOff:
		return embedModeOff
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
func startBatchEmbeddings(ctx context.Context, s *store.Store, repoRoot string, symbols []index.Symbol, fingerprint string) (string, error) {
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
	toEmbed := filterEmbeddableSymbols(symbols)
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

	// Update metadata
	if err := s.SetMeta("indexed_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store timestamp: %w", err)
	}

	// Build summary
	nMod := len(changes.Modified)
	nAdd := len(changes.Added)
	nDel := len(changes.Deleted)
	fmt.Fprintf(os.Stderr, "Incremental: updated %d files (%d modified, %d added, %d deleted)\n",
		nMod+nAdd+nDel, nMod, nAdd, nDel)

	symCount, _, _, _ := s.GetStats()
	return w.WriteResponse(indexResponse(absDir, start, symCount, orphanSuggestion(incResult)))
}

// runDeleteOnlyIndex handles the case where only files were deleted.
// Skips the expensive go/packages load entirely — just removes dead rows from the DB.
func runDeleteOnlyIndex(s *store.Store, changes *index.ChangeResult, absDir string, start time.Time, w *output.Writer) error {
	nDel := len(changes.Deleted)
	fmt.Fprintf(os.Stderr, "Delete-only: removing %d files (skipping package load)\n", nDel)

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

	fmt.Fprintf(os.Stderr, "Deleted %d files\n", nDel)

	symCount, _, _, _ := s.GetStats()
	return w.WriteResponse(indexResponse(absDir, start, symCount, orphanSuggestion(incResult)))
}

// indexResponse builds a standard index command response.
func indexResponse(absDir string, start time.Time, symCount int, suggestions []output.Suggestion) output.Response[any] {
	return output.Response[any]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Suggestions: suggestions,
		Meta: output.Meta{
			Command:    cmdNameIndex,
			RepoRoot:   absDir,
			IndexState: output.IndexFresh,
			Ms:         time.Since(start).Milliseconds(),
			Total:      symCount,
		},
	}
}

// orphanSuggestion returns a suggestion to rebuild if orphaned refs exist.
func orphanSuggestion(inc *store.IncrementalResult) []output.Suggestion {
	if inc == nil || inc.OrphanedRefs == 0 {
		return nil
	}
	return []output.Suggestion{{
		Command:     "snipe index --force",
		Description: fmt.Sprintf("Full rebuild to clear %d orphaned refs", inc.OrphanedRefs),
		Priority:    3,
		Condition:   "incremental_orphans",
	}}
}

// computeImportPageRank loads the intra-repo imports graph, runs PageRank,
// and persists results into graph_metrics. Best-effort: callers log+continue.
func computeImportPageRank(s *store.Store) error {
	t0 := time.Now()
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	scores := graphmetrics.PageRank(g, 0.85, 50)
	if err := s.WriteGraphMetrics("imports", "pagerank", scores); err != nil {
		return fmt.Errorf("write graph metrics: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: pagerank computed for %d packages in %dms\n",
		len(scores), time.Since(t0).Milliseconds())
	return nil
}

// computeImportCoupling loads the intra-repo imports graph (production-only,
// excluding edges from _test.go files) and persists per-package Ca and Ce
// (afferent and efferent coupling) as separate metric kinds. Foundation for
// instability (I = Ce/(Ca+Ce)) and other Martin-style architecture metrics.
func computeImportCoupling(s *store.Store) error {
	t0 := time.Now()
	g, err := graphmetrics.LoadImportsGraphOpts(s, false)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	coup := graphmetrics.ComputeCoupling(g)
	ca := make(map[string]float64, len(coup))
	ce := make(map[string]float64, len(coup))
	in := make(map[string]float64, len(coup))
	for n, c := range coup {
		ca[n] = float64(c.Ca)
		ce[n] = float64(c.Ce)
		in[n] = c.I()
	}
	if err := s.WriteGraphMetrics("imports", "ca", ca); err != nil {
		return fmt.Errorf("write ca: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "ce", ce); err != nil {
		return fmt.Errorf("write ce: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "instability", in); err != nil {
		return fmt.Errorf("write instability: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: Ca/Ce/I computed for %d packages in %dms\n",
		len(coup), time.Since(t0).Milliseconds())
	return nil
}

// computeAbstractness persists per-package A = abstract / total exported type
// decls (production files only). "Abstract" = interface decls; "total" =
// interface + struct + type. Pure-interface pkg → 1.0; pure-impl → 0.0;
// zero-type pkgs are skipped (no row written). Pairs with instability for
// distance-from-main-sequence (D = |A + I - 1|).
func computeAbstractness(s *store.Store) error {
	t0 := time.Now()
	rows, err := s.DB().Query(`
		SELECT pkg_path,
		       SUM(CASE WHEN kind = 'interface' THEN 1 ELSE 0 END) AS abstract,
		       SUM(CASE WHEN kind IN ('interface','struct','type') THEN 1 ELSE 0 END) AS total
		FROM symbols
		WHERE pkg_path IS NOT NULL AND pkg_path != ''
		  AND file_path NOT LIKE '%_test.go'
		  AND substr(name, 1, 1) GLOB '[A-Z]'
		GROUP BY pkg_path
		HAVING total > 0
	`)
	if err != nil {
		return fmt.Errorf("query abstractness: %w", err)
	}
	defer rows.Close()
	values := make(map[string]float64)
	for rows.Next() {
		var pkg string
		var abstract, total int
		if err := rows.Scan(&pkg, &abstract, &total); err != nil {
			return fmt.Errorf("scan abstractness: %w", err)
		}
		values[pkg] = float64(abstract) / float64(total)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iter abstractness: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "abstractness", values); err != nil {
		return fmt.Errorf("write abstractness: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: abstractness computed for %d packages in %dms\n",
		len(values), time.Since(t0).Milliseconds())
	return nil
}

// computeLCOM4 persists per-package LCOM4 = number of connected components in
// the intra-package call graph (production funcs/methods, no _test.go). Nodes
// are top-level functions in a package; edges are intra-package func→func
// references (treated undirected). LCOM4 = 1 means the package is internally
// connected; LCOM4 > 1 surfaces split candidates (cross-reference with
// `snipe boundary`). Singleton packages (1 function) report LCOM4 = 1.
func computeLCOM4(s *store.Store) error {
	t0 := time.Now()

	pkgNodes := make(map[string]map[string]struct{})
	nodeRows, err := s.DB().Query(`
		SELECT pkg_path, id FROM symbols
		WHERE kind IN ('func','method')
		  AND pkg_path IS NOT NULL AND pkg_path != ''
		  AND pkg_path NOT LIKE '%.test'
		  AND file_path NOT LIKE '%_test.go'
	`)
	if err != nil {
		return fmt.Errorf("query lcom4 nodes: %w", err)
	}
	for nodeRows.Next() {
		var pkg, id string
		if err := nodeRows.Scan(&pkg, &id); err != nil {
			nodeRows.Close()
			return fmt.Errorf("scan lcom4 node: %w", err)
		}
		set, ok := pkgNodes[pkg]
		if !ok {
			set = make(map[string]struct{})
			pkgNodes[pkg] = set
		}
		set[id] = struct{}{}
	}
	nodeRows.Close()
	if err := nodeRows.Err(); err != nil {
		return fmt.Errorf("iter lcom4 nodes: %w", err)
	}

	type edge struct{ pkg, src, dst string }
	edgeRows, err := s.DB().Query(`
		SELECT s_enc.pkg_path, r.enclosing_id, r.symbol_id
		FROM refs r
		JOIN symbols s_enc ON s_enc.id = r.enclosing_id
		JOIN symbols s_tgt ON s_tgt.id = r.symbol_id
		WHERE r.enclosing_id IS NOT NULL
		  AND r.enclosing_id != r.symbol_id
		  AND s_enc.pkg_path = s_tgt.pkg_path
		  AND s_enc.pkg_path IS NOT NULL AND s_enc.pkg_path != ''
		  AND s_enc.pkg_path NOT LIKE '%.test'
		  AND s_enc.kind IN ('func','method')
		  AND s_tgt.kind IN ('func','method')
		  AND s_enc.file_path NOT LIKE '%_test.go'
		  AND s_tgt.file_path NOT LIKE '%_test.go'
	`)
	if err != nil {
		return fmt.Errorf("query lcom4 edges: %w", err)
	}
	pkgEdges := make(map[string][]edge)
	for edgeRows.Next() {
		var e edge
		if err := edgeRows.Scan(&e.pkg, &e.src, &e.dst); err != nil {
			edgeRows.Close()
			return fmt.Errorf("scan lcom4 edge: %w", err)
		}
		pkgEdges[e.pkg] = append(pkgEdges[e.pkg], e)
	}
	edgeRows.Close()
	if err := edgeRows.Err(); err != nil {
		return fmt.Errorf("iter lcom4 edges: %w", err)
	}

	values := make(map[string]float64, len(pkgNodes))
	for pkg, nodes := range pkgNodes {
		parent := make(map[string]string, len(nodes))
		for n := range nodes {
			parent[n] = n
		}
		find := func(x string) string {
			for parent[x] != x {
				parent[x] = parent[parent[x]]
				x = parent[x]
			}
			return x
		}
		union := func(a, b string) {
			ra, rb := find(a), find(b)
			if ra != rb {
				parent[ra] = rb
			}
		}
		for _, e := range pkgEdges[pkg] {
			if _, ok := nodes[e.src]; !ok {
				continue
			}
			if _, ok := nodes[e.dst]; !ok {
				continue
			}
			union(e.src, e.dst)
		}
		roots := make(map[string]struct{}, len(nodes))
		for n := range nodes {
			roots[find(n)] = struct{}{}
		}
		values[pkg] = float64(len(roots))
	}

	if err := s.WriteGraphMetrics("imports", "lcom4", values); err != nil {
		return fmt.Errorf("write lcom4: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: LCOM4 computed for %d packages in %dms\n",
		len(values), time.Since(t0).Milliseconds())
	return nil
}

// computeCycloRollups persists per-package McCabe cyclomatic-complexity
// rollups (sum, p95, max) under the imports graph as cyclo_sum/p95/max;
// `snipe metrics --kind=cyclo` joins them at read time.
// Hot-package threshold: p95 > 10 OR max > 20.
func computeCycloRollups(s *store.Store, symbols []index.Symbol) error {
	return writeComplexityRollups(s, "cyclo", index.PackageCycloRollups(symbols))
}

// computeCognitiveRollups is the cognitive-complexity counterpart, stored as
// cognitive_sum/p95/max for `snipe metrics --kind=cognitive`.
func computeCognitiveRollups(s *store.Store, symbols []index.Symbol) error {
	return writeComplexityRollups(s, "cognitive", index.PackageCognitiveRollups(symbols))
}

// writeComplexityRollups persists a per-package rollup map under the imports
// graph as {prefix}_sum/{prefix}_p95/{prefix}_max in one place, so cyclo and
// cognitive share the storage shape.
func writeComplexityRollups(s *store.Store, prefix string, rollups map[string]index.CycloRollup) error {
	if len(rollups) == 0 {
		return nil
	}
	t0 := time.Now()
	sum := make(map[string]float64, len(rollups))
	p95 := make(map[string]float64, len(rollups))
	maxv := make(map[string]float64, len(rollups))
	for pkg, r := range rollups {
		sum[pkg] = float64(r.Sum)
		p95[pkg] = r.P95
		maxv[pkg] = float64(r.Max)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_sum", sum); err != nil {
		return fmt.Errorf("write %s_sum: %w", prefix, err)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_p95", p95); err != nil {
		return fmt.Errorf("write %s_p95: %w", prefix, err)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_max", maxv); err != nil {
		return fmt.Errorf("write %s_max: %w", prefix, err)
	}
	fmt.Fprintf(os.Stderr, "metrics: %s rollups computed for %d packages in %dms\n",
		prefix, len(rollups), time.Since(t0).Milliseconds())
	return nil
}

// computeFileCyclo aggregates per-symbol McCabe complexity into per-file
// sum and max, stored under a synthetic "files" graph. `snipe hotspots`
// joins this against file_churn (path-keyed) to rank complexity ×
// change-frequency. Test files are excluded to match computeCycloRollups —
// hotspots targets production risk. Paths are relativized to repo_root so
// they line up with the churn and symbols tables.
func computeFileCyclo(s *store.Store, symbols []index.Symbol) error {
	root, err := s.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("read repo_root: %w", err)
	}
	sum := make(map[string]float64)
	maxv := make(map[string]float64)
	for i := range symbols {
		sym := &symbols[i]
		if sym.Kind != index.KindFunc && sym.Kind != index.KindMethod {
			continue
		}
		if strings.HasSuffix(sym.FilePath, "_test.go") {
			continue
		}
		rel := relToRoot(root, sym.FilePath)
		c := float64(sym.Cyclo)
		sum[rel] += c
		if c > maxv[rel] {
			maxv[rel] = c
		}
	}
	if len(sum) == 0 {
		return nil
	}
	if err := s.WriteGraphMetrics("files", "cyclo_sum", sum); err != nil {
		return fmt.Errorf("write file cyclo_sum: %w", err)
	}
	if err := s.WriteGraphMetrics("files", "cyclo_max", maxv); err != nil {
		return fmt.Errorf("write file cyclo_max: %w", err)
	}
	return nil
}

// relToRoot returns absPath relative to root, falling back to absPath when
// root is empty or the paths share no base (mirrors store.toRelPath).
func relToRoot(root, absPath string) string {
	if root == "" {
		return absPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

// computeChurn derives per-file git change-frequency (the temporal axis of
// the Tornhill hotspot model) and replaces the file_churn table. It always
// writes — even an empty result — so the table tracks HEAD: a repo that
// loses git history, or a non-git checkout, ends with no stale churn. Git
// absence is a clean no-op inside gitchurn.Walk, not an error.
func computeChurn(s *store.Store) error {
	root, err := s.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("read repo_root: %w", err)
	}
	if root == "" {
		return nil
	}
	t0 := time.Now()
	rows, err := gitchurn.Walk(root)
	if err != nil {
		return fmt.Errorf("walk git history: %w", err)
	}
	churn := make([]store.FileChurn, len(rows))
	for i, r := range rows {
		churn[i] = store.FileChurn{
			Path:        r.Path,
			Commits:     r.Commits,
			Authors:     r.Authors,
			FirstSeen:   r.FirstSeen,
			LastChanged: r.LastChanged,
			Score:       r.Score,
		}
	}
	if err := s.WriteFileChurn(churn); err != nil {
		return fmt.Errorf("write file churn: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: git churn computed for %d files in %dms\n",
		len(churn), time.Since(t0).Milliseconds())
	return nil
}

// computeImportSCCs loads the intra-repo imports graph, computes nontrivial
// strongly-connected components via Tarjan, and persists them into graph_sccs.
func computeImportSCCs(s *store.Store) error {
	t0 := time.Now()
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	sccs := graphmetrics.SCC(g)
	if err := s.WriteSCCs("imports", sccs); err != nil {
		return fmt.Errorf("write graph sccs: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: detected %d nontrivial SCCs in %dms\n",
		len(sccs), time.Since(t0).Milliseconds())
	return nil
}

// computeImportHITS loads the intra-repo imports graph, runs Kleinberg HITS,
// and persists hub and authority vectors as separate graph_metrics rows.
func computeImportHITS(s *store.Store) error {
	t0 := time.Now()
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	hubs, auths := graphmetrics.HITS(g, 50)
	if err := s.WriteGraphMetrics("imports", "hub", hubs); err != nil {
		return fmt.Errorf("write hub metrics: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "authority", auths); err != nil {
		return fmt.Errorf("write authority metrics: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: HITS computed for %d packages in %dms\n",
		len(hubs), time.Since(t0).Milliseconds())
	return nil
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
		resp := output.Response[any]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  nil,
			Meta: output.Meta{
				Command:    cmdNameIndex,
				RepoRoot:   absDir,
				IndexState: output.IndexFresh,
				Ms:         time.Since(start).Milliseconds(),
				Total:      symCount,
			},
		}
		return &changeDetection{result: skipResultSkipped}, w.WriteResponse(resp)
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

// computeImportDegreeAndEigenvector loads the intra-repo imports graph, computes
// in-degree, out-degree, and eigenvector centrality, and persists each as its
// own metric row.
func computeImportDegreeAndEigenvector(s *store.Store) error {
	t0 := time.Now()
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return fmt.Errorf("load imports graph: %w", err)
	}
	in := graphmetrics.InDegree(g)
	out := graphmetrics.OutDegree(g)
	eig := graphmetrics.EigenvectorCentrality(g, 50)
	if err := s.WriteGraphMetrics("imports", "in_degree", in); err != nil {
		return fmt.Errorf("write in_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "out_degree", out); err != nil {
		return fmt.Errorf("write out_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("imports", "eigenvector", eig); err != nil {
		return fmt.Errorf("write eigenvector: %w", err)
	}
	fmt.Fprintf(os.Stderr, "metrics: degree+eigenvector computed for %d packages in %dms\n",
		len(in), time.Since(t0).Milliseconds())
	return nil
}

// computeCallsGraphMetrics loads the call graph and computes the same family of
// metrics as the imports graph: PageRank, HITS (hub+authority), in/out degree,
// eigenvector centrality, betweenness, and SCCs. Persisted with graph_kind="calls".
//
// Topo sort is intentionally skipped — recursion is normal in code, and a cycle
// witness on the call graph isn't useful output. SCC already surfaces recursion
// clusters in a more digestible form.
func computeCallsGraphMetrics(s *store.Store) error {
	t0 := time.Now()
	// Exclude _test.go-rooted edges so test helpers (testutil.NewStore,
	// setupTestEnv) don't dominate centrality metrics. snipe-7fk.
	g, err := graphmetrics.LoadCallsGraphOpts(s, false)
	if err != nil {
		return fmt.Errorf("load calls graph: %w", err)
	}

	// SCC — recursion clusters.
	sccs := graphmetrics.SCC(g)
	if err := s.WriteSCCs("calls", sccs); err != nil {
		return fmt.Errorf("write calls sccs: %w", err)
	}

	// PageRank.
	pr := graphmetrics.PageRank(g, 0.85, 50)
	if err := s.WriteGraphMetrics("calls", "pagerank", pr); err != nil {
		return fmt.Errorf("write calls pagerank: %w", err)
	}

	// HITS hub + authority.
	hubs, auths := graphmetrics.HITS(g, 50)
	if err := s.WriteGraphMetrics("calls", "hub", hubs); err != nil {
		return fmt.Errorf("write calls hub: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "authority", auths); err != nil {
		return fmt.Errorf("write calls authority: %w", err)
	}

	// Degree + eigenvector.
	in := graphmetrics.InDegree(g)
	out := graphmetrics.OutDegree(g)
	eig := graphmetrics.EigenvectorCentrality(g, 50)
	if err := s.WriteGraphMetrics("calls", "in_degree", in); err != nil {
		return fmt.Errorf("write calls in_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "out_degree", out); err != nil {
		return fmt.Errorf("write calls out_degree: %w", err)
	}
	if err := s.WriteGraphMetrics("calls", "eigenvector", eig); err != nil {
		return fmt.Errorf("write calls eigenvector: %w", err)
	}

	// Brandes betweenness — O(V*E). Call graph is small (~600 nodes, ~2.7k edges
	// on snipe itself) so this stays well under a second.
	bc := graphmetrics.Betweenness(g)
	if err := s.WriteGraphMetrics("calls", "betweenness", bc); err != nil {
		return fmt.Errorf("write calls betweenness: %w", err)
	}

	fmt.Fprintf(os.Stderr, "metrics: calls graph computed for %d symbols in %dms\n",
		len(in), time.Since(t0).Milliseconds())
	return nil
}
