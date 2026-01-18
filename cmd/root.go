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
	jsonOutput    bool
	humanOutput   bool
	compactOutput bool
	limit         int
	offset        int
	contextLines  int
	withBody      bool
	noBody        bool
	withSiblings  bool
	noSiblings    bool
	summaryOnly   bool

	maxTokens int

	// response_format mode: concise, detailed, or summary
	responseFormat string

	// KG integration
	withKGHints bool

	// loadedConfig holds the merged config (loaded lazily)
	loadedConfig *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "snipe",
	Short: "Code navigation CLI for LLMs",
	Long: `snipe: Fast, deterministic Go code navigation for LLMs.

Commands:
  index     Build SQLite index from Go source (run first)
  def       Jump to definition (by name, position, or hex ID)
  refs      Find all references to a symbol
  callers   Find functions that call a symbol
  callees   Find functions that a symbol calls
  show      Expand symbol by hex ID (from previous results)
  search    Text search via ripgrep (no index needed)
  doctor    Check index health and freshness

Key flags (apply to most commands):
  --at file:line:col   Query by position (what you have from errors/output)
  --no-body            Exclude function/method body (body included by default)
  --no-siblings        Exclude sibling declarations (siblings included by default for def)
  --limit N            Cap results (default 50)
  --summary            Return counts per file instead of full results
  --max-tokens N       Truncate output to token budget

Output format:
  JSON with {results, meta, error}. Use --human for debugging.
  Each result has edit_target for direct file:line:col handoff.
  Use --compact for minified JSON (~30% smaller, auto-enabled for pipes).`,
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

		// Auto-enable compact mode when output is not a TTY (piped to another process)
		if !cmd.Flags().Changed("compact") && !term.IsTerminal(int(os.Stdout.Fd())) {
			compactOutput = true
		}

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
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", true, "JSON output (default)")
	rootCmd.PersistentFlags().BoolVar(&humanOutput, "human", false, "Pretty-printed for debugging")
	rootCmd.PersistentFlags().BoolVar(&compactOutput, "compact", false, "Minified JSON (auto-enabled for pipes)")
	rootCmd.PersistentFlags().IntVar(&limit, "limit", 50, "Cap results")
	rootCmd.PersistentFlags().IntVar(&offset, "offset", 0, "Skip first N results (for pagination)")
	rootCmd.PersistentFlags().IntVar(&contextLines, "context", 3, "Lines of context around match")
	rootCmd.PersistentFlags().BoolVar(&withBody, "with-body", true, "Include full enclosing function body (default: true)")
	rootCmd.PersistentFlags().BoolVar(&noBody, "no-body", false, "Exclude function/method body")
	rootCmd.PersistentFlags().BoolVar(&withSiblings, "with-siblings", true, "Include sibling declarations in same file (for def)")
	rootCmd.PersistentFlags().BoolVar(&noSiblings, "no-siblings", false, "Exclude sibling declarations")
	rootCmd.PersistentFlags().BoolVar(&summaryOnly, "summary", false, "Show summary stats only (counts per file)")

	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 0, "Maximum tokens in output (0 = unlimited)")

	// response_format flag for go_symbol parity
	rootCmd.PersistentFlags().StringVar(&responseFormat, "format", "", "Output format: concise (minimal), detailed (full metadata), summary (file counts)")

	// KG integration flag
	rootCmd.PersistentFlags().BoolVar(&withKGHints, "kg-hints", false, "Include hints from Orca knowledge graph")
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

// GetOutputConfig returns the current output configuration
func GetOutputConfig() (json bool, human bool, compact bool, lim int, off int, ctx int, body bool, siblings bool, summary bool) {
	// noBody overrides withBody (default true)
	effectiveBody := withBody && !noBody
	// noSiblings overrides withSiblings (default true)
	effectiveSiblings := withSiblings && !noSiblings
	return jsonOutput, humanOutput, compactOutput, limit, offset, contextLines, effectiveBody, effectiveSiblings, summaryOnly
}

// GetResponseFormat returns the response format mode.
// Returns the effective format, resolving --summary flag as an alias.
func GetResponseFormat() ResponseFormat {
	// --summary flag is an alias for --format=summary
	if summaryOnly && responseFormat == "" {
		return FormatSummary
	}
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
	default:
		// Default: use base values
		return baseBody, baseSiblings, baseContext
	}
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
