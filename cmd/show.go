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

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show symbol details by ID",
	Long: `Shows full details for a symbol given its ID.

Use this to expand deferred IDs from other command outputs.

Examples:
  snipe show abc123def456`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
}

func init() {
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, human, _, _, contextLines, withBody, withSiblings, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	symbolID := args[0]

	// Validate symbol ID format (16-char hex string)
	if len(symbolID) != 16 {
		return w.WriteError("show", &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid symbol ID: must be 16 characters",
		})
	}
	if _, err := hex.DecodeString(symbolID); err != nil {
		return w.WriteError("show", &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid symbol ID: must be hexadecimal",
		})
	}

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("show", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if store.IsIndexing(dbPath) {
		return w.WriteError("show", output.NewIndexInProgressError())
	}
	if !store.Exists(dbPath) {
		return w.WriteError("show", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("show", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	// Look up by ID
	sym, err := query.LookupByID(s.DB(), symbolID)
	if err != nil {
		return w.WriteError("show", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if sym == nil {
		return w.WriteError("show", &output.Error{
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

	resp := output.Response[output.Result]{
		Results: []output.Result{result},
		Meta: output.Meta{
			Command:       "show",
			Query:         map[string]string{"id": symbolID},
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         1,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
