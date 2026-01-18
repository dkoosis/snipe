package cmd

import (
	"encoding/hex"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var callersCmd = &cobra.Command{
	Use:   "callers [symbol|id]",
	Short: "Find functions that call a symbol",
	Long: `Finds all functions that call a given symbol.

Accepts symbol name or 16-char hex ID (auto-detected).
Use --with-body to include caller function bodies.

Examples:
  snipe callers ProcessOrder      # Find callers by name
  snipe callers a3f2c1de89ab0123  # Find callers by hex ID (auto-detected)
  snipe callers --id abc123       # Explicit --id flag
  snipe callers ProcessOrder --with-body  # Include caller bodies`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCallers,
}

var callersID string

func init() {
	callersCmd.Flags().StringVar(&callersID, "id", "", "Symbol ID to look up")
	rootCmd.AddCommand(callersCmd)
}

func runCallers(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, human, lim, off, contextLines, withBody, _, summary := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	if len(args) == 0 && callersID == "" {
		return w.WriteError("callers", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name or --id",
		})
	}

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("callers", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if store.IsIndexing(dbPath) {
		return w.WriteError("callers", output.NewIndexInProgressError())
	}
	if !store.Exists(dbPath) {
		return w.WriteError("callers", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("callers", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	if callersID != "" {
		symbolID = callersID
		queryInfo = map[string]string{"id": callersID}
	} else {
		name := args[0]

		// Check if input looks like a symbol ID (16-char hex string)
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				goto findCallers
			}
		}

		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("callers", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError("callers", output.NewNotFoundError(name))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, sym := range symbols {
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError("callers", output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
	}

findCallers:

	// Find callers
	calls, err := query.FindCallers(s.DB(), symbolID, lim, off)
	if err != nil {
		return w.WriteError("callers", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - show the caller functions
	results := make([]output.Result, len(calls))
	tokenEstimate := 0
	var degraded []string

	for i, call := range calls {
		// Use callee name length for accurate call site range
		nameLen := len(call.CalleeName)
		if nameLen == 0 {
			nameLen = 1
		}
		callRange := output.Range{
			Start: output.Position{Line: call.CallLine, Col: call.CallCol},
			End:   output.Position{Line: call.CallLine, Col: call.CallCol + nameLen},
		}
		// Use relative path for output, absolute for file operations
		filePath := call.CallerFileRel
		if filePath == "" {
			filePath = call.CallerFile
		}
		result := output.Result{
			ID:         call.CallerID,
			File:       filePath,
			FileAbs:    call.CallerFile,
			Range:      callRange,
			Kind:       call.CallerKind,
			Name:       call.CallerName,
			Match:      call.CallerSignature.String,
			EditTarget: output.FormatEditTargetWithHash(filePath, call.CallerFile, callRange),
		}

		// Add caller body if requested (from the caller's definition, not call site)
		if withBody {
			callerSym, lookupErr := query.LookupByID(s.DB(), call.CallerID)
			if lookupErr == nil && callerSym != nil {
				callerResult := callerSym.ToResult()
				if err := output.AddBody(&callerResult); err != nil {
					degraded = append(degraded, "body_extraction_failed")
				}
				result.Body = callerResult.Body
			}
		}

		if contextLines > 0 && !withBody {
			if err := output.AddContext(&result, contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}

		results[i] = result
		tokenEstimate += output.EstimateTokens(call.CallerSignature.String)
		if result.Body != "" {
			tokenEstimate = output.EstimateTokens(result.Body)
		}
	}

	// Deduplicate degraded messages
	degraded = uniqueStrings(degraded)

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	// If summary mode, return condensed output
	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Results: []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    "callers",
				Query:      queryInfo,
				RepoRoot:   dir,
				IndexState: query.CheckIndexState(s.DB(), dir, Version),
				Degraded:   degraded,
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Offset:     off,
				Limit:      lim,
				Truncated:  len(results) >= lim,
			},
		}
		return w.WriteResponse(summaryResp)
	}

	// Recalculate token estimate after truncation
	tokenEstimate = 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "callers",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Offset:        off,
			Limit:         lim,
			Truncated:     len(results) >= lim || tokenTruncated,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
