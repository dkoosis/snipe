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
	contextLines int
	withBody     bool
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
	rootCmd.PersistentFlags().IntVar(&contextLines, "context", 3, "Lines of context around match")
	rootCmd.PersistentFlags().BoolVar(&withBody, "with-body", false, "Include full enclosing function body")
}

// GetOutputConfig returns the current output configuration
func GetOutputConfig() (json bool, human bool, lim int, ctx int, body bool) {
	return jsonOutput, humanOutput, limit, contextLines, withBody
}
