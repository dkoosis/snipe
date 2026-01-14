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

	_, human, lim, contextLines, _ := GetOutputConfig()
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
	calls, err := query.FindCallees(s.DB(), symbolID, lim)
	if err != nil {
		return w.WriteError("callees", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - show the callee functions
	results := make([]output.Result, len(calls))
	tokenEstimate := 0

	for i, call := range calls {
		result := output.Result{
			ID:   call.CalleeID,
			File: call.CalleeFile,
			Range: output.Range{
				Start: output.Position{Line: call.CallLine, Col: call.CallCol},
				End:   output.Position{Line: call.CallLine, Col: call.CallCol + 10},
			},
			Kind:  call.CalleeKind,
			Name:  call.CalleeName,
			Match: call.CalleeSignature.String,
			EditTarget: output.FormatEditTarget(call.CallerFile, output.Range{
				Start: output.Position{Line: call.CallLine, Col: call.CallCol},
				End:   output.Position{Line: call.CallLine, Col: call.CallCol + 10},
			}),
		}

		if contextLines > 0 {
			// Use the call site for context (in the caller's file)
			result.File = call.CallerFile
			_ = output.AddContext(&result, contextLines)
		}

		results[i] = result
		tokenEstimate += output.EstimateTokens(call.CalleeSignature.String)
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "callees",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Truncated:     len(results) >= lim,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
