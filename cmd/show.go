package cmd

import (
	"encoding/hex"
	"os"
	"time"

	"github.com/spf13/cobra"

	ctxpkg "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var showCmd = &cobra.Command{
	Use:     "show <id>",
	Short:   "Show symbol details by ID",
	GroupID: categoryCore,
	Long: `Shows full details for a symbol given its 16-char hex ID.

Use this to expand IDs from other command outputs — part of the
hex-ID chaining workflow (def -> callers -> show).

Examples:
  snipe show a3f2c1de89ab0123              # Expand a result by ID
  snipe show a3f2c1de89ab0123 --with-body  # Include function body`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
}

func init() {
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, _, _, contextLines, withBody, withSiblings := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	symbolID := args[0]

	// Validate symbol ID format (16-char hex string)
	if len(symbolID) != 16 {
		return w.WriteError(cmdNameShow, &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid symbol ID: must be 16 characters",
		})
	}
	if _, err := hex.DecodeString(symbolID); err != nil {
		return w.WriteError(cmdNameShow, &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid symbol ID: must be hexadecimal",
		})
	}

	s, dir, err := OpenStore(w, cmdNameShow)
	if err != nil {
		return err
	}
	defer s.Close()

	// Look up by ID
	sym, err := query.LookupByID(s.DB(), symbolID)
	if err != nil {
		return w.WriteError(cmdNameShow, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if sym == nil {
		return w.WriteError(cmdNameShow, &output.Error{
			Code:    output.ErrNotFound,
			Message: "symbol not found: " + symbolID,
		})
	}

	result := sym.ToResultWithHints(s.DB())
	var degraded []string

	// Add full body if requested
	if withBody {
		if err := output.AddBody(&result); err != nil {
			degraded = append(degraded, "body_extraction_failed")
		}
	}

	// Add context lines if requested (only if not showing full body)
	if contextLines > 0 && !withBody {
		if err := output.AddContext(&result, contextLines); err != nil {
			degraded = append(degraded, "context_extraction_failed")
		}
	}

	// Add role classification in detailed format
	if GetResponseFormat() == FormatDetailed {
		role := ctxpkg.InferRoleForSymbol(s.DB(), sym.ID, sym.Name, sym.Kind,
			sym.Signature.String, sym.PkgPath, sym.FilePath)
		result.Role = string(role)
	}

	// Add sibling declarations if requested
	if withSiblings {
		siblings, err := query.FindSiblings(s.DB(), sym.FilePath, sym.Kind, sym.ID, 20)
		if err != nil {
			degraded = append(degraded, "siblings_query_failed")
		} else if len(siblings) > 0 {
			result.Siblings = siblings
		}
	}

	tokenEstimate := output.EstimateTokens(result.Match)
	if result.Body != "" {
		tokenEstimate = output.EstimateTokens(result.Body)
	}

	results := []output.Result{result}

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
		tokenEstimate = 0
		for i := range results {
			tokenEstimate += output.EstimateResultTokens(&results[i])
		}
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: output.SuggestionsForDef(&result),
		Meta: output.Meta{
			Command:       cmdNameShow,
			Query:         map[string]string{"id": symbolID},
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			TokenEstimate: tokenEstimate,
			StaleFiles:    staleFiles,
			Truncated:     tokenTruncated,
		},
	}

	return w.WriteResponse(resp)
}
