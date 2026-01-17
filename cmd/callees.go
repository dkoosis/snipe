package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var calleesCmd = &cobra.Command{
	Use:   "callees [symbol]",
	Short: "Find functions that a symbol calls",
	Long: `Finds all functions called by a given symbol.

Examples:
  snipe callees ProcessOrder      # Find callees by name
  snipe callees --id abc123       # Find callees by symbol ID`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCallees,
}

var calleesID string

func init() {
	calleesCmd.Flags().StringVar(&calleesID, "id", "", "Symbol ID to look up")
	rootCmd.AddCommand(calleesCmd)
}

func runCallees(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, human, lim, off, contextLines, withBody, _, summary := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	if len(args) == 0 && calleesID == "" {
		return w.WriteError("callees", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name or --id",
		})
	}

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("callees", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if store.IsIndexing(dbPath) {
		return w.WriteError("callees", output.NewIndexInProgressError())
	}
	if !store.Exists(dbPath) {
		return w.WriteError("callees", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("callees", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	if calleesID != "" {
		symbolID = calleesID
		queryInfo = map[string]string{"id": calleesID}
	} else {
		name := args[0]
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("callees", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError("callees", output.NewNotFoundError(name))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, sym := range symbols {
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError("callees", output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
	}

	// Find callees
	calls, err := query.FindCallees(s.DB(), symbolID, lim, off)
	if err != nil {
		return w.WriteError("callees", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - show the callee functions
	results := make([]output.Result, len(calls))
	tokenEstimate := 0
	var degraded []string

	for i, call := range calls {
		// Use callee name length for accurate call site range
		nameLen := len(call.CalleeName)
		if nameLen == 0 {
			nameLen = 1
		}
		// Show the call site (where the callee is invoked from the caller)
		callSiteRange := output.Range{
			Start: output.Position{Line: call.CallLine, Col: call.CallCol},
			End:   output.Position{Line: call.CallLine, Col: call.CallCol + nameLen},
		}
		// Use relative path for output, absolute for file operations
		filePath := call.CallerFileRel
		if filePath == "" {
			filePath = call.CallerFile
		}
		result := output.Result{
			ID:         call.CalleeID,
			File:       filePath, // Call site location
			FileAbs:    call.CallerFile,
			Range:      callSiteRange,
			Kind:       call.CalleeKind,
			Name:       call.CalleeName,
			Match:      call.CalleeSignature.String,
			EditTarget: output.FormatEditTargetWithHash(filePath, call.CallerFile, callSiteRange),
		}

		// Add callee body if requested (from the callee's definition, not call site)
		if withBody {
			calleeSym, lookupErr := query.LookupByID(s.DB(), call.CalleeID)
			if lookupErr == nil && calleeSym != nil {
				calleeResult := calleeSym.ToResult()
				if err := output.AddBody(&calleeResult); err != nil {
					degraded = append(degraded, "body_extraction_failed")
				}
				result.Body = calleeResult.Body
			}
		}

		if contextLines > 0 && !withBody {
			if err := output.AddContext(&result, contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}

		results[i] = result
		tokenEstimate += output.EstimateTokens(call.CalleeSignature.String)
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
				Command:    "callees",
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
			Command:       "callees",
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
