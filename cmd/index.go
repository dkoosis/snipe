package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Build or update the code index",
	Long:  `Builds a SQLite index of symbols, references, and call graph for fast navigation.`,
	Args:  cobra.MaximumNArgs(1),
	RunE:  runIndex,
}

func init() {
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
	_, human, _, _, _ := GetOutputConfig()
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

	// Extract refs
	fmt.Fprintf(os.Stderr, "Extracting references...\n")
	refs, err := index.ExtractRefs(result, symbols)
	if err != nil {
		return fmt.Errorf("extract refs: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d references\n", len(refs))

	// Extract call graph
	fmt.Fprintf(os.Stderr, "Building call graph...\n")
	edges, err := index.ExtractCallGraph(result, symbols)
	if err != nil {
		return fmt.Errorf("extract call graph: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Found %d call edges\n", len(edges))

	// Write to store
	fmt.Fprintf(os.Stderr, "Writing index...\n")
	if err := s.WriteIndex(symbols, refs, edges); err != nil {
		return fmt.Errorf("write index: %w", err)
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

	return w.WriteResponse(resp)
}
