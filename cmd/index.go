package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

var indexCmd = &cobra.Command{
	Use:     "index [path]",
	Short:   "Build or update the code index",
	GroupID: "index",
	Long: `Builds a SQLite index of symbols, references, and call graph for fast navigation.

By default, generates embeddings (auto mode) and LLM-based symbol purposes (enrich).
Use --embed-mode=off and --enrich=false to disable.

Embedding modes:
  auto     - Use batch API for initial indexing (async), realtime for incremental
  batch    - Force batch API (async, up to 12h completion)
  realtime - Force realtime API (sync, may timeout on large codebases)
  off      - Skip embedding generation`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIndex,
}

// Embedding mode constants.
const (
	embedModeAuto     = "auto"
	embedModeBatch    = "batch"
	embedModeRealtime = "realtime"
	embedModeOff      = "off"
)

var (
	withEmbed  bool   // Legacy flag, kept for compatibility
	embedMode  string // New flag: auto, batch, realtime, off
	withEnrich bool   // Generate LLM-based symbol purposes (placeholder, not yet wired)
	forceIndex bool   // Force full re-index even if no changes detected
)

func init() {
	// Legacy flag - kept for backwards compatibility
	defaultEmbed := embed.HasCredentials()
	indexCmd.Flags().BoolVar(&withEmbed, "embed", defaultEmbed, "Generate embeddings (deprecated: use --embed-mode)")
	indexCmd.Flags().StringVar(&embedMode, "embed-mode", "auto", "Embedding mode: auto, batch, realtime, off")
	indexCmd.Flags().BoolVar(&withEnrich, "enrich", false, "Generate LLM-based symbol purposes (placeholder, not yet wired)")
	indexCmd.Flags().BoolVar(&forceIndex, "force", false, "Force full re-index even if no changes detected")
	rootCmd.AddCommand(indexCmd)
}

func runIndex(cmd *cobra.Command, args []string) error {
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

	// Setup output writer
	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

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

	result, err := index.Load(index.LoadConfig{
		Context:  GetContext(),
		Dir:      absDir,
		Patterns: []string{"./..."},
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
		return runIncrementalIndex(cmd, s, result, symbols, pkgDocs, detection.changes, absDir, start, w)
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
	if err := s.SetMeta("repo_root", absDir); err != nil {
		return fmt.Errorf("store repo root: %w", err)
	}
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
		status, err := startBatchEmbeddings(absDir, symbols, fp.Combined)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: batch embedding failed: %v\n", err)
			embedStatus = batchStatusFailed
		} else {
			embedStatus = status
		}
	case embedModeRealtime:
		ec, err := generateEmbeddings(s, symbols)
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
	for _, sym := range symbols {
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
func generateEmbeddings(s *store.Store, symbols []index.Symbol) (int, error) {
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
		embeddings, err := client.Embed(texts, "document")
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

// startBatchEmbeddings initiates async batch embedding via Voyage API.
// fingerprint identifies the index generation that these embeddings belong to.
func startBatchEmbeddings(repoRoot string, symbols []index.Symbol, fingerprint string) (string, error) {
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

	if state != nil && (state.Status == "validating" || state.Status == "in_progress") {
		// Check if batch is stale (stuck for too long)
		age := time.Since(state.UpdatedAt)
		switch {
		case age > batchStaleThreshold:
			// Try to verify actual status from Voyage API
			fmt.Fprintf(os.Stderr, "Batch %s has been %q for %v, checking actual status...\n",
				state.BatchID, state.Status, age.Round(time.Minute))

			actualStatus, err := client.GetBatchStatus(state.BatchID)
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
					// Check if batch was created for a different index generation
					if !state.MatchesFingerprint(fingerprint) {
						fmt.Fprintf(os.Stderr, "  Batch was created for a different index version, discarding stale results...\n")
						if clearErr := client.ClearState(); clearErr != nil {
							return "", fmt.Errorf("clear stale batch state: %w", clearErr)
						}
						// Fall through to start new batch
					} else {
						// Batch completed but results never processed — auto-recover
						fmt.Fprintf(os.Stderr, "  Batch completed, recovering results...\n")

						// Update state with output file info from API
						state.Status = batchStatusCompleted
						state.OutputFileID = actualStatus.OutputFileID
						state.ErrorFileID = actualStatus.ErrorFileID
						state.Completed = actualStatus.RequestCounts.Completed
						state.Failed = actualStatus.RequestCounts.Failed
						state.UpdatedAt = time.Now()

						dbPath := store.DefaultIndexPath(repoRoot)
						count, dlErr := downloadAndSaveEmbeddings(client, state, dbPath)
						if dlErr != nil {
							// Download failed (expired output, API error) — clear and restart
							fmt.Fprintf(os.Stderr, "  Recovery failed: %v\n", dlErr)
							fmt.Fprintf(os.Stderr, "  Clearing state and starting fresh...\n")
							if clearErr := client.ClearState(); clearErr != nil {
								return "", fmt.Errorf("clear failed batch state: %w", clearErr)
							}
							// Fall through to start new batch
						} else {
							fmt.Fprintf(os.Stderr, "  Recovered %d embeddings from completed batch\n", count)
							if clearErr := client.ClearState(); clearErr != nil {
								fmt.Fprintf(os.Stderr, "  Warning: failed to clear state: %v\n", clearErr)
							}
							return "batch_recovered", nil
						}
					}
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
	fileResp, err := client.UploadFile(jsonlPath)
	if err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Uploaded file_id: %s\n", fileResp.ID)

	// Create batch
	fmt.Fprintf(os.Stderr, "  Creating batch job...\n")
	batchResp, err := client.CreateBatch(fileResp.ID)
	if err != nil {
		return "", fmt.Errorf("create batch: %w", err)
	}
	fmt.Fprintf(os.Stderr, "  Created batch_id: %s (status: %s)\n", batchResp.ID, batchResp.Status)

	// Save state for polling
	newState := &embed.BatchState{
		BatchID:          batchResp.ID,
		InputFileID:      fileResp.ID,
		Status:           batchResp.Status,
		Total:            len(toEmbed),
		Completed:        0,
		Failed:           0,
		CreatedAt:        time.Now(),
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
func runIncrementalIndex(_ *cobra.Command, s *store.Store, result *index.LoadResult, allSymbols []index.Symbol, pkgDocs []index.PackageDoc, changes *index.ChangeResult, absDir string, start time.Time, w *output.Writer) error {
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
	for _, sym := range allSymbols {
		if onlyFiles[sym.FilePath] {
			changedSymbols = append(changedSymbols, sym)
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

	// Write incremental update
	fmt.Fprintf(os.Stderr, "Writing incremental index...\n")
	incResult, err := s.WriteIndexIncremental(changedSymbols, refs, edges, imports, changedFiles, changes.Deleted)
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

	// Remove symbols, refs, edges, imports, embeddings, purposes, and file entries
	// for deleted files (all within a single transaction in WriteIndexIncremental)
	incResult, err := s.WriteIndexIncremental(nil, nil, nil, nil, nil, changes.Deleted)
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
			Command:    "index",
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
				Command:    "index",
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
