package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Build or update the code index",
	Long: `Builds a SQLite index of symbols, references, and call graph for fast navigation.

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
	withEmbed bool   // Legacy flag, kept for compatibility
	embedMode string // New flag: auto, batch, realtime, off
)

func init() {
	// Legacy flag - kept for backwards compatibility
	defaultEmbed := embed.HasCredentials()
	indexCmd.Flags().BoolVar(&withEmbed, "embed", defaultEmbed, "Generate embeddings (deprecated: use --embed-mode)")
	indexCmd.Flags().StringVar(&embedMode, "embed-mode", "auto", "Embedding mode: auto, batch, realtime, off")
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
	human, compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human, compact)

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

	// Load packages
	fmt.Fprintf(os.Stderr, "Loading packages from %s...\n", absDir)
	loadStart := time.Now()

	result, err := index.Load(index.LoadConfig{
		Dir:      absDir,
		Patterns: []string{"./..."},
	})
	if err != nil {
		return fmt.Errorf("load packages: %w", err)
	}

	loadMs := time.Since(loadStart).Milliseconds()
	fmt.Fprintf(os.Stderr, "Loaded %d packages in %dms\n", len(result.Packages), loadMs)

	// Report any load errors
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", e)
	}

	// Extract symbols
	fmt.Fprintf(os.Stderr, "Extracting symbols...\n")
	symbols, err := index.ExtractSymbols(result)
	if err != nil {
		return fmt.Errorf("extract symbols: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d symbols\n", len(symbols))

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
	files, err := index.ExtractFileInfo(result)
	if err != nil {
		return fmt.Errorf("extract file info: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Hashed %d files\n", len(files))

	// Write to store
	fmt.Fprintf(os.Stderr, "Writing index...\n")
	if err := s.WriteIndex(symbols, refs, edges); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Write imports
	if err := s.WriteImports(imports); err != nil {
		return fmt.Errorf("write imports: %w", err)
	}

	// Write file hashes
	if err := s.WriteFiles(files); err != nil {
		return fmt.Errorf("write files: %w", err)
	}

	// Store fingerprint
	if err := s.SetMeta("fingerprint", fp.Combined); err != nil {
		return fmt.Errorf("store fingerprint: %w", err)
	}
	if err := s.SetMeta("indexed_at", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("store timestamp: %w", err)
	}
	if err := s.SetMeta("repo_root", absDir); err != nil {
		return fmt.Errorf("store repo root: %w", err)
	}

	// Determine effective embedding mode
	effectiveMode := resolveEmbedMode(embedMode, withEmbed, s)

	// Generate embeddings based on mode
	var embedCount int
	var embedStatus string
	switch effectiveMode {
	case embedModeOff:
		embedStatus = "disabled"
	case embedModeBatch:
		status, err := startBatchEmbeddings(absDir, symbols)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: batch embedding failed: %v\n", err)
			embedStatus = "failed"
		} else {
			embedStatus = status
		}
	case embedModeRealtime:
		ec, err := generateEmbeddings(s, symbols)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: embedding generation failed: %v\n", err)
			embedStatus = "failed"
		} else {
			embedCount = ec
			embedStatus = "completed"
		}
	}

	// Output result
	resp := output.Response[any]{
		Results: nil,
		Meta: output.Meta{
			Command:    "index",
			RepoRoot:   absDir,
			IndexState: output.IndexFresh,
			Ms:         time.Since(start).Milliseconds(),
			Total:      len(symbols),
		},
	}

	if embedCount > 0 {
		fmt.Fprintf(os.Stderr, "Generated %d embeddings\n", embedCount)
	} else if embedStatus == "batch_started" {
		fmt.Fprintf(os.Stderr, "Batch embedding started (async). Use 'snipe embed-status' to check progress.\n")
	}

	return w.WriteResponse(resp)
}

// generateEmbeddings creates embeddings for symbols with signatures.
func generateEmbeddings(s *store.Store, symbols []index.Symbol) (int, error) {
	client, err := embed.NewClient()
	if err != nil {
		return 0, err
	}

	fmt.Fprintf(os.Stderr, "Generating embeddings with %s...\n", client.Model())

	// Filter symbols worth embedding (functions, methods, types with signatures/docs)
	var toEmbed []index.Symbol
	for _, sym := range symbols {
		// Embed functions, methods, and types
		switch sym.Kind {
		case index.KindFunc, index.KindMethod, index.KindType, index.KindInterface, index.KindStruct:
			if sym.Signature != "" || sym.Doc != "" {
				toEmbed = append(toEmbed, sym)
			}
		case index.KindVar, index.KindConst, index.KindField:
			// Skip - these typically don't have meaningful signatures
		}
	}

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
			// Combine name, signature, and doc for richer embeddings
			text := sym.Name
			if sym.Signature != "" {
				text += " " + sym.Signature
			}
			if sym.Doc != "" {
				text += " " + sym.Doc
			}
			texts[j] = text
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

// startBatchEmbeddings initiates async batch embedding via Voyage API.
func startBatchEmbeddings(repoRoot string, symbols []index.Symbol) (string, error) {
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
		fmt.Fprintf(os.Stderr, "Batch embedding already in progress (batch_id: %s, status: %s)\n", state.BatchID, state.Status)
		return "batch_in_progress", nil
	}

	// Filter symbols worth embedding
	var toEmbed []embed.SymbolText
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
				toEmbed = append(toEmbed, embed.SymbolText{
					ID:   sym.ID,
					Text: text,
				})
			}
		case index.KindVar, index.KindConst, index.KindField:
			// Skip - these typically don't have meaningful signatures
		}
	}

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
		BatchID:     batchResp.ID,
		InputFileID: fileResp.ID,
		Status:      batchResp.Status,
		Total:       len(toEmbed),
		Completed:   0,
		Failed:      0,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Model:       client.Model(),
	}
	if err := client.SaveState(newState); err != nil {
		return "", fmt.Errorf("save state: %w", err)
	}

	// Clean up local JSONL file
	os.Remove(jsonlPath)

	return "batch_started", nil
}
