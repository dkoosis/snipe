package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	contextFormat string
	contextFull   bool
)

var contextCmd = &cobra.Command{
	Use:   "context [path]",
	Short: "Generate Claude-optimized project context",
	Long: `Generates a structured JSON/YAML output describing the project architecture,
files, and key symbols - optimized for providing context to Claude.

The output includes:
- Project metadata (name, root, build commands)
- Architecture components and data flows
- Files organized by concern
- Key types and functions
- Generation metadata

Examples:
  snipe context                    # Generate context for current directory
  snipe context .                  # Same as above
  snipe context --format=yaml      # Output as YAML
  snipe context --full             # Include all symbols, not just key ones`,
	Args: cobra.MaximumNArgs(1),
	RunE: runContext,
}

func init() {
	contextCmd.Flags().StringVar(&contextFormat, "format", "json", "Output format: json or yaml")
	contextCmd.Flags().BoolVar(&contextFull, "full", false, "Include all symbols, not just key ones")
	rootCmd.AddCommand(contextCmd)
}

func runContext(cmd *cobra.Command, args []string) error {
	// Determine directory
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Find project root
	projectRoot := findProjectRoot(absDir)
	if projectRoot == "" {
		return fmt.Errorf("not in a git repository")
	}

	// Open index
	indexPath := store.DefaultIndexPath(projectRoot)
	if !store.Exists(indexPath) {
		return fmt.Errorf("index not found at %s\nRun 'snipe index' first", indexPath)
	}

	s, err := store.Open(indexPath)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer s.Close()

	// Generate context
	cfg := context.GenerateConfig{
		RepoRoot: projectRoot,
		DB:       s.DB(),
		Full:     contextFull,
	}

	ctx, err := context.Generate(cfg)
	if err != nil {
		return fmt.Errorf("generate context: %w", err)
	}

	// Output in requested format
	switch contextFormat {
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(ctx); err != nil {
			return fmt.Errorf("encode yaml: %w", err)
		}
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ctx); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format: %s (use json or yaml)", contextFormat)
	}

	return nil
}
