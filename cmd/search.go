package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/search"
)

var searchCmd = &cobra.Command{
	Use:   "search <pattern>",
	Short: "Text search via ripgrep",
	Long:  `Searches for a pattern using ripgrep. Works without an index.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

func init() {
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	start := time.Now()
	pattern := args[0]

	_, human, lim, _, ctx, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	// Get current directory
	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("search", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	results, err := search.Search(dir, pattern, lim, ctx)
	if err != nil {
		return w.WriteError("search", &output.Error{
			Code:    output.ErrRgNotFound,
			Message: err.Error(),
		})
	}

	// Estimate tokens
	tokenEstimate := 0
	for _, r := range results {
		tokenEstimate += output.EstimateTokens(r.Match)
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "search",
			Query:         map[string]string{"pattern": pattern},
			IndexState:    output.IndexMissing, // search doesn't use index
			Degraded:      []string{"no_index"},
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Truncated:     len(results) >= lim,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
