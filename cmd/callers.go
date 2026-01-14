package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var callersCmd = &cobra.Command{
	Use:   "callers [symbol]",
	Short: "Find functions that call a symbol",
	Long: `Finds all functions that call a given symbol.

Examples:
  snipe callers ProcessOrder      # Find callers by name
  snipe callers --id abc123       # Find callers by symbol ID`,
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

	_, human, lim, contextLines, _ := GetOutputConfig()
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

	// Find callers
	calls, err := query.FindCallers(s.DB(), symbolID, lim)
	if err != nil {
		return w.WriteError("callers", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - show the caller functions
	results := make([]output.Result, len(calls))
	tokenEstimate := 0

	for i, call := range calls {
		result := output.Result{
			ID:   call.CallerID,
			File: call.CallerFile,
			Range: output.Range{
				Start: output.Position{Line: call.CallLine, Col: call.CallCol},
				End:   output.Position{Line: call.CallLine, Col: call.CallCol + 10},
			},
			Kind:  call.CallerKind,
			Name:  call.CallerName,
			Match: call.CallerSignature.String,
			EditTarget: output.FormatEditTarget(call.CallerFile, output.Range{
				Start: output.Position{Line: call.CallLine, Col: call.CallCol},
				End:   output.Position{Line: call.CallLine, Col: call.CallCol + 10},
			}),
		}

		if contextLines > 0 {
			_ = output.AddContext(&result, contextLines)
		}

		results[i] = result
		tokenEstimate += output.EstimateTokens(call.CallerSignature.String)
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "callers",
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
