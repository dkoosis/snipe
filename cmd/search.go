package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/search"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

var identifierRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.]*$`)

var searchFile string

var searchCmd = &cobra.Command{
	Use:     "search <pattern>",
	Short:   "Text search via ripgrep",
	GroupID: "core",
	Long: `Searches for a pattern using ripgrep. Works without an index.

If no text matches are found for an identifier-like query and an index exists,
falls back to symbol name lookup in the index. If still no results and embeddings
are available, falls back to semantic similarity search.

Use 'snipe sim' directly for more control over semantic search parameters.

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

	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Get current directory
	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("search", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	// Resolve to project root for index lookup (D3)
	root := util.FindProjectRoot(dir)
	if root == "" {
		root = dir
	}

	// Open store once — used for both enrichment and potential semantic fallback
	dbPath := store.DefaultIndexPath(root)
	var s *store.Store
	if store.Exists(dbPath) && !store.IsIndexing(dbPath) {
		if opened, err := store.Open(dbPath); err == nil {
			s = opened
			defer s.Close()
		}
	}

	var globs []string
	if searchFile != "" {
		globs = append(globs, searchFile)
	} else if identifierRe.MatchString(pattern) {
		// Identifier-like queries: restrict to Go source to avoid docs/changelogs
		globs = append(globs, "*.go")
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

	// Index name fallback — when rg returns 0 results for an identifier-like pattern,
	// check the symbols table directly. Faster than semantic search, no API call needed.
	indexFallbackFound := false
	var decisionPath []string
	if len(results) == 0 && searchFile == "" && s != nil && identifierRe.MatchString(pattern) {
		symRows, lookupErr := query.LookupByName(s.DB(), pattern)
		if lookupErr == nil && len(symRows) > 0 {
			for i := range symRows {
				if i >= lim {
					break
				}
				results = append(results, symRows[i].ToResult())
			}
			decisionPath = []string{"rg:0_results", fmt.Sprintf("index_fallback:%d_results", len(results))}
			indexFallbackFound = true
		}
	}

	// Semantic fallback — only on zero rg results, no --file filter.
	// Threshold 0.3 is intentionally lower than sim's default: fallback is a safety net
	// for "find the thing that does X" queries, not a precision tool.
	// Note: identifier-like patterns add a *.go glob to rg but the semantic search
	// operates over the full embedding space regardless of file type.
	usedFallback := false
	if len(results) == 0 && searchFile == "" && !indexFallbackFound && s != nil && embed.HasCredentials() {
		client, clientErr := embed.NewClient()
		if clientErr != nil {
			decisionPath = append(decisionPath, "rg:0_results", "sim:client_error")
		} else {
			simResults, simDur, simErr := embed.Search(cmd.Context(), pattern, s, client, lim, 0.3)
			if simErr == nil && len(simResults) > 0 {
				results = simResults
				decisionPath = []string{
					"rg:0_results",
					fmt.Sprintf("sim:%d_results:%dms", len(simResults), simDur.Milliseconds()),
				}
				usedFallback = true
			} else if simErr != nil {
				decisionPath = append(decisionPath, "rg:0_results", "sim:error")
			}
		}
	}

	// Enrich rg results with index metadata if available (skip for semantic results)
	var indexState output.IndexState
	enriched := false
	switch {
	case s != nil && !usedFallback && !indexFallbackFound:
		for i := range results {
			line := results[i].Range.Start.Line
			// Enclosing func/method/type — also fills doc-comment hits via line+20 fallback.
			if enc := query.FindEnclosingSymbol(s.DB(), results[i].File, line); enc != nil {
				results[i].Name = enc.Name
				results[i].Kind = enc.Kind
				if enc.Receiver.Valid && enc.Receiver.String != "" {
					results[i].Receiver = enc.Receiver.String
				}
				enriched = true
			} else if sym := query.FindSymbolAtPosition(s.DB(), results[i].File, line); sym != nil {
				// Fallback: any symbol (var/const) when no func/method/type contains the hit.
				results[i].Name = sym.Name
				results[i].Kind = sym.Kind
				if sym.Receiver.Valid && sym.Receiver.String != "" {
					results[i].Receiver = sym.Receiver.String
				}
				enriched = true
			}
		}
		indexState = query.CheckIndexState(s.DB(), dir, Version)
	case usedFallback && s != nil:
		// TODO: semantic results reference indexed symbols — CheckFileStaleness
		// is relevant here but search has never included stale_files. Add when
		// search gets staleness support generally.
		indexState = query.CheckIndexState(s.DB(), dir, Version)
		enriched = true
	case indexFallbackFound && s != nil:
		indexState = query.CheckIndexState(s.DB(), dir, Version)
		enriched = true
	}

	// Score, sort, and apply selection (only for rg results)
	if !usedFallback {
		output.ScoreAndSort(results, pattern)
		results = ApplySelection(results)
	}

	// Augmentation — when rg has hits but the query is conceptual (identifier-like
	// single word), the right symbol may not match the query exactly. Two paths:
	//   1. Substring on indexed symbol names (covers "Args" → ExactArgs).
	//   2. Stemmed substring (covers "matcher" → "match" → FuzzyMatchV2,
	//      "staleness" → "stale" → findStaleNuggets).
	//   3. Semantic search when embeddings exist (vague concepts).
	// Augments are appended at the end so they don't displace top rg hits.
	if !usedFallback && !indexFallbackFound && len(results) > 0 && searchFile == "" &&
		s != nil && identifierRe.MatchString(pattern) && !strings.Contains(pattern, ".") {
		existing := make(map[string]bool, len(results)+8)
		for i := range results {
			existing[fmt.Sprintf("%s:%d:%s", results[i].File, results[i].Range.Start.Line, results[i].Name)] = true
		}
		appendUnique := func(sym query.SymbolRow) bool {
			r := sym.ToResult()
			key := fmt.Sprintf("%s:%d:%s", r.File, r.Range.Start.Line, r.Name)
			if existing[key] {
				return false
			}
			existing[key] = true
			results = append(results, r)
			return true
		}

		augAdded := 0
		// Substring on full pattern.
		if subRows, subErr := query.LookupByNameSubstring(s.DB(), pattern, 15); subErr == nil {
			for i := range subRows {
				if appendUnique(subRows[i]) {
					augAdded++
				}
			}
		}
		// Stemmed substring — covers "matcher"→"match", "staleness"→"stale".
		if stem := query.StemQuery(pattern); stem != "" && stem != strings.ToLower(pattern) {
			if stemRows, stemErr := query.LookupByNameSubstring(s.DB(), stem, 15); stemErr == nil {
				for i := range stemRows {
					if appendUnique(stemRows[i]) {
						augAdded++
					}
				}
			}
		}
		if augAdded > 0 {
			decisionPath = append(decisionPath, fmt.Sprintf("name_augment:%d_added", augAdded))
			enriched = true
		}

		// Semantic augment if embeddings present.
		if embed.HasCredentials() {
			if client, clientErr := embed.NewClient(); clientErr == nil {
				simResults, simDur, simErr := embed.Search(cmd.Context(), pattern, s, client, 5, 0.4)
				if simErr == nil && len(simResults) > 0 {
					simAdded := 0
					for i := range simResults {
						key := fmt.Sprintf("%s:%d:%s", simResults[i].File, simResults[i].Range.Start.Line, simResults[i].Name)
						if existing[key] {
							continue
						}
						existing[key] = true
						results = append(results, simResults[i])
						simAdded++
					}
					if simAdded > 0 {
						decisionPath = append(decisionPath,
							fmt.Sprintf("sim_augment:%d_added:%dms", simAdded, simDur.Milliseconds()))
						enriched = true
					}
				}
			}
		}
	}

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
		searchDegraded = []string{"rg_only"}
	}

	// If summary mode, return condensed output
	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:      "search",
				Query:        searchQueryInfo(pattern),
				IndexState:   searchIndexState,
				Degraded:     searchDegraded,
				DecisionPath: decisionPath,
				Ms:           time.Since(start).Milliseconds(),
				Total:        summaryData.Total,
				Truncated:    len(results) >= lim,
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
		Suggestions: output.SuggestionsForSearch(pattern, len(results), usedFallback),
		Meta: output.Meta{
			Command:       "search",
			Query:         searchQueryInfo(pattern),
			IndexState:    searchIndexState,
			Degraded:      searchDegraded,
			DecisionPath:  decisionPath,
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
