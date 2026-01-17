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

Use --embed to generate semantic embeddings for similarity search.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIndex,
}

var withEmbed bool

func init() {
	indexCmd.Flags().BoolVar(&withEmbed, "embed", false, "Generate embeddings for semantic search")
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
	_, human, _, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	// Compute fingerprint
	fp, err := index.ComputeFingerprint(absDir, Version)
	if err != nil {
		return fmt.Errorf("compute fingerprint: %w", err)
	}

	// Open or create store
	dbPath := store.DefaultIndexPath(absDir)
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

	// Generate embeddings if requested
	var embedCount int
	if withEmbed {
		ec, err := generateEmbeddings(s, symbols)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: embedding generation failed: %v\n", err)
		} else {
			embedCount = ec
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
