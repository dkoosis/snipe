package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/config"
	snipemcp "github.com/dkoosis/snipe/internal/mcp"
)

var (
	// Global flags
	jsonOutput   bool
	humanOutput  bool
	limit        int
	offset       int
	contextLines int
	withBody     bool
	withSiblings bool
	summaryOnly  bool

	maxTokens    int

	mcpMode      bool


	// loadedConfig holds the merged config (loaded lazily)
	loadedConfig *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "snipe",
	Short: "Code navigation CLI for LLMs",
	Long: `snipe provides fast, deterministic code navigation optimized for LLM consumption.
JSON-first output, position-addressed queries, static indexing for speed.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Skip config loading in MCP mode
		if mcpMode {
			return nil
		}

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

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpMode {
			server := snipemcp.NewServer(Version)
			return server.Run(context.Background())
		}
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
	rootCmd.PersistentFlags().IntVar(&limit, "limit", 50, "Cap results")
	rootCmd.PersistentFlags().IntVar(&offset, "offset", 0, "Skip first N results (for pagination)")
	rootCmd.PersistentFlags().IntVar(&contextLines, "context", 3, "Lines of context around match")
	rootCmd.PersistentFlags().BoolVar(&withBody, "with-body", false, "Include full enclosing function body")
	rootCmd.PersistentFlags().BoolVar(&withSiblings, "with-siblings", false, "Include sibling declarations in same file")
	rootCmd.PersistentFlags().BoolVar(&summaryOnly, "summary", false, "Show summary stats only (counts per file)")

	rootCmd.PersistentFlags().IntVar(&maxTokens, "max-tokens", 0, "Maximum tokens in output (0 = unlimited)")

	rootCmd.Flags().BoolVar(&mcpMode, "mcp", false, "Run as MCP server (stdio transport)")

}

// GetOutputConfig returns the current output configuration
func GetOutputConfig() (json bool, human bool, lim int, off int, ctx int, body bool, siblings bool, summary bool) {
	return jsonOutput, humanOutput, limit, offset, contextLines, withBody, withSiblings, summaryOnly
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
