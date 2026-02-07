package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/search"
	"github.com/dkoosis/snipe/internal/store"
)

var searchFile string

var searchCmd = &cobra.Command{
	Use:     "search <pattern>",
	Short:   "Text search via ripgrep",
	GroupID: "core",
	Long: `Searches for a pattern using ripgrep. Works without an index.

Examples:
  snipe search "func.*Error"              # Search all files
  snipe search "TODO" --file "*.go"       # Search only Go files
  snipe search "Handler" --file store.go  # Search in specific file`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().StringVar(&searchFile, "file", "", "Glob pattern to filter files (e.g., \"*.go\", \"store.go\")")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	start := time.Now()
	pattern := args[0]

	compact, lim, _, ctx, _, _ := GetOutputConfig()
	format := GetResponseFormat()

	// Apply format overrides
	_, _, ctx = ApplyFormatOverrides(format, false, false, ctx)
	summary := format == FormatSummary

	w := output.NewWriter(os.Stdout, compact)

	// Get current directory
	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("search", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	var globs []string
	if searchFile != "" {
		globs = append(globs, searchFile)
	}
	results, err := search.Search(dir, pattern, lim, ctx, globs...)
	if err != nil {
		code := output.ErrInternal
		if strings.Contains(err.Error(), "not found") {
			code = output.ErrRgNotFound
		}
		return w.WriteError("search", &output.Error{
			Code:    code,
			Message: err.Error(),
		})
	}

	// Enrich search results with index metadata if available
	var indexState output.IndexState
	enriched := false
	dbPath := store.DefaultIndexPath(dir)
	if store.Exists(dbPath) && !store.IsIndexing(dbPath) {
		if s, err := store.Open(dbPath); err == nil {
			for i := range results {
				if sym := query.FindSymbolAtPosition(s.DB(), results[i].File, results[i].Range.Start.Line); sym != nil {
					results[i].Name = sym.Name
					results[i].Kind = sym.Kind
					if sym.Receiver.Valid && sym.Receiver.String != "" {
						results[i].Receiver = sym.Receiver.String
					}
					enriched = true
				}
			}
			indexState = query.CheckIndexState(s.DB(), dir, Version)
			s.Close()
		}
	}

	// Score, sort, and apply selection
	output.ScoreAndSort(results, pattern)
	results = ApplySelection(results)

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	// Determine index state and degraded flags for response metadata
	searchIndexState := output.IndexNotUsed
	var searchDegraded []string
	if enriched {
		searchIndexState = indexState
	} else {
		searchDegraded = []string{"no_index"}
	}

	// If summary mode, return condensed output
	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    "search",
				Query:      searchQueryInfo(pattern),
				IndexState: searchIndexState,
				Degraded:   searchDegraded,
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Truncated:  len(results) >= lim,
			},
		}
		return w.WriteResponse(summaryResp)
	}

	// Estimate tokens
	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: output.SuggestionsForSearch(pattern, len(results)),
		Meta: output.Meta{
			Command:       "search",
			Query:         searchQueryInfo(pattern),
			IndexState:    searchIndexState,
			Degraded:      searchDegraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Truncated:     len(results) >= lim || tokenTruncated,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}

func searchQueryInfo(pattern string) map[string]string {
	q := map[string]string{"pattern": pattern}
	if searchFile != "" {
		q["file"] = searchFile
	}
	return q
}
