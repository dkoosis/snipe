package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
)

var rootCmd = &cobra.Command{
	Use:   "snipe",
	Short: "Code navigation CLI for LLMs",
	Long: `snipe provides fast, deterministic code navigation optimized for LLM consumption.
JSON-first output, position-addressed queries, static indexing for speed.`,
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
}

// GetOutputConfig returns the current output configuration
func GetOutputConfig() (json bool, human bool, lim int, off int, ctx int, body bool, siblings bool, summary bool) {
	return jsonOutput, humanOutput, limit, offset, contextLines, withBody, withSiblings, summaryOnly
}
