package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dkoosis/snipe/internal/config"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	// Global flags
	humanOutput   bool
	limit         int
	offset        int
	contextLines  int
	noBody        bool
	noSiblings    bool
	signatureOnly bool
	maxTokens     int

	// response_format mode: concise, detailed, or summary
	responseFormat string

	// KG integration
	withKGHints bool

	// Internal: auto-compact when piped
	autoCompact bool

	// loadedConfig holds the merged config (loaded lazily)
	loadedConfig *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "snipe",
	Short: "Code navigation CLI for LLMs",
	Long: `snipe: Fast, deterministic Go code navigation for LLMs.

Core commands:
  index     Build SQLite index from Go source (run first)
  def       Jump to definition
  refs      Find all references
  callers   Find functions that call a symbol
  callees   Find functions called by a symbol
  search    Text search via ripgrep (no index needed)

Output: JSON with {results, meta, error}. Use --human for debugging.
Auto-compacts when piped. Each result has edit_target for file:line:col handoff.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Load config and apply defaults if flags weren't explicitly set
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		cfg, err := config.Load(cwd)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		loadedConfig = cfg

		// Apply config defaults only if flags weren't explicitly set
		if !cmd.Flags().Changed("limit") && cfg.Limit > 0 {
			limit = cfg.Limit
		}
		if !cmd.Flags().Changed("context") && cfg.ContextLines > 0 {
			contextLines = cfg.ContextLines
		}

		// Auto-compact when output is piped (not a TTY)
		autoCompact = !term.IsTerminal(int(os.Stdout.Fd()))

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// No subcommand specified, show help
		return cmd.Help()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&humanOutput, "human", false, "Pretty-print for debugging")
	rootCmd.PersistentFlags().IntVar(&limit, "limit", 50, "Cap results")
	rootCmd.PersistentFlags().IntVar(&offset, "offset", 0, "Pagination offset")
	rootCmd.PersistentFlags().IntVar(&contextLines, "context", 3, "Context lines around match")
	rootCmd.PersistentFlags().BoolVar(&noBody, "no-body", false, "Exclude function body")
	rootCmd.PersistentFlags().BoolVar(&noSiblings, "no-siblings", false, "Exclude sibling declarations")
	rootCmd.PersistentFlags().BoolVar(&signatureOnly, "signature-only", false, "Return only signature (no body, no context)")
	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 0, "Token budget (0 = unlimited)")
	rootCmd.PersistentFlags().StringVar(&responseFormat, "format", "", "concise | detailed | summary")
	rootCmd.PersistentFlags().BoolVar(&withKGHints, "kg-hints", false, "Include Orca KG hints")
}

// ResponseFormat represents output format modes for go_symbol parity.
type ResponseFormat string

const (
	// FormatDefault uses command-specific defaults.
	FormatDefault ResponseFormat = ""
	// FormatConcise strips bodies, minimal metadata.
	FormatConcise ResponseFormat = "concise"
	// FormatDetailed includes full metadata and all hints.
	FormatDetailed ResponseFormat = "detailed"
	// FormatSummary aggregates results by file (counts only).
	FormatSummary ResponseFormat = "summary"
)

// GetOutputConfig returns the current output configuration.
// Returns: human, compact, limit, offset, contextLines, withBody, withSiblings
func GetOutputConfig() (human bool, compact bool, lim int, off int, ctx int, body bool, siblings bool) {
	// Apply signature-only override
	if signatureOnly {
		return humanOutput, autoCompact, limit, offset, 0, false, false
	}
	return humanOutput, autoCompact, limit, offset, contextLines, !noBody, !noSiblings
}

// GetResponseFormat returns the response format mode.
func GetResponseFormat() ResponseFormat {
	return ResponseFormat(responseFormat)
}

// ApplyFormatOverrides adjusts output config based on --format flag.
// Returns (withBody, withSiblings, contextLines) based on format mode.
func ApplyFormatOverrides(format ResponseFormat, baseBody, baseSiblings bool, baseContext int) (bool, bool, int) {
	switch format {
	case FormatConcise:
		// Concise: no body, no siblings, minimal context
		return false, false, 0
	case FormatDetailed:
		// Detailed: everything enabled, full context
		return true, true, baseContext
	case FormatSummary:
		// Summary: no body needed (just counts)
		return false, false, 0
	case FormatDefault:
		// Default: use base values
		return baseBody, baseSiblings, baseContext
	}
	// Unreachable, but satisfies exhaustive check
	return baseBody, baseSiblings, baseContext
}

// GetWithKGHints returns whether KG hints should be included.
func GetWithKGHints() bool {
	return withKGHints
}

// GetMaxTokens returns the max-tokens flag value (0 = unlimited)
func GetMaxTokens() int {
	return maxTokens
}

// uniqueStrings removes duplicates from a string slice, preserving order.
func uniqueStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// GetConfig returns the loaded configuration.
func GetConfig() *config.Config {
	if loadedConfig == nil {
		return config.DefaultConfig()
	}
	return loadedConfig
}

// OpenStore opens the index for query commands.
// Returns the store, working directory, and any error.
func OpenStore(w *output.Writer, cmdName string) (*store.Store, string, error) {
	dir, err := os.Getwd()
	if err != nil {
		if w != nil {
			_ = w.WriteError(cmdName, &output.Error{
				Code:    output.ErrInternal,
				Message: "failed to get working directory: " + err.Error(),
			})
		}
		return nil, "", err
	}

	dbPath := store.DefaultIndexPath(dir)

	// Check if indexing is in progress
	if store.IsIndexing(dbPath) {
		if w != nil {
			_ = w.WriteError(cmdName, output.NewIndexInProgressError())
		}
		return nil, dir, fmt.Errorf("indexing in progress")
	}

	// Check for missing index
	if !store.Exists(dbPath) {
		if w != nil {
			_ = w.WriteError(cmdName, output.NewMissingIndexError())
		}
		return nil, dir, fmt.Errorf("index missing")
	}

	s, err := store.Open(dbPath)
	if err != nil {
		if w != nil {
			_ = w.WriteError(cmdName, &output.Error{
				Code:    output.ErrInternal,
				Message: "failed to open index: " + err.Error(),
			})
		}
		return nil, dir, err
	}

	return s, dir, nil
}
