package cmd

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	refsAt string
)

var refsCmd = &cobra.Command{
	Use:   "refs [symbol]",
	Short: "Find all references to a symbol",
	Long: `Finds all references to a symbol by name or position.

Examples:
  snipe refs ProcessOrder          # Find by name
  snipe refs --at main.go:42:12    # Find at position`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRefs,
}

func init() {
	refsCmd.Flags().StringVar(&refsAt, "at", "", "Position to look up (file:line:col)")
	rootCmd.AddCommand(refsCmd)
}

func runRefs(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, human, lim, off, contextLines, withBody, summary := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	// Need either a symbol name or --at position
	if len(args) == 0 && refsAt == "" {
		return w.WriteError("refs", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name or --at position",
		})
	}

	// Find repo root and open store
	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("refs", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if !store.Exists(dbPath) {
		return w.WriteError("refs", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("refs", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	if refsAt != "" {
		// Resolve position
		pos, err := query.ParsePosition(refsAt)
		if err != nil {
			return w.WriteError("refs", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		// Make path absolute if relative
		if !filepath.IsAbs(pos.File) {
			pos.File = filepath.Join(dir, pos.File)
		}

		symbolID, err = query.ResolvePosition(s.DB(), pos)
		if err != nil {
			return w.WriteError("refs", &output.Error{
				Code:    output.ErrNotFound,
				Message: err.Error(),
			})
		}
		queryInfo = map[string]string{"at": refsAt}
	} else {
		// Look up by name
		name := args[0]
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("refs", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError("refs", output.NewNotFoundError(name))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, s := range symbols {
				candidates[i] = s.ToCandidate()
			}
			return w.WriteError("refs", output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
	}

	// Find all references
	refs, err := query.FindRefs(s.DB(), symbolID, lim, off)
	if err != nil {
		return w.WriteError("refs", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results
	results := make([]output.Result, len(refs))
	tokenEstimate := 0

	for i, ref := range refs {
		result := output.Result{
			ID:   ref.ID,
			File: ref.FilePath,
			Range: output.Range{
				Start: output.Position{Line: ref.Line, Col: ref.Col},
				End:   output.Position{Line: ref.Line, Col: ref.Col + 10}, // Approximate
			},
			Kind:  "ref",
			Match: ref.Snippet,
			EditTarget: output.FormatEditTarget(ref.FilePath, output.Range{
				Start: output.Position{Line: ref.Line, Col: ref.Col},
				End:   output.Position{Line: ref.Line, Col: ref.Col + 10},
			}),
		}

		// Add enclosing info if available
		if ref.EnclosingID.Valid {
			result.Enclosing = &output.Enclosing{
				ID:        ref.EnclosingID.String,
				Kind:      ref.EnclosingKind,
				Name:      ref.EnclosingName,
				Signature: ref.EnclosingSignature,
			}

			// Add enclosing function body if requested
			if withBody {
				encSym, lookupErr := query.LookupByID(s.DB(), ref.EnclosingID.String)
				if lookupErr == nil && encSym != nil {
					encResult := encSym.ToResult()
					_ = output.AddBody(&encResult)
					result.Body = encResult.Body
				}
			}
		}

		// Add context lines if requested (only if not showing full body)
		if contextLines > 0 && !withBody {
			_ = output.AddContext(&result, contextLines)
		}

		results[i] = result
		tokenEstimate += output.EstimateTokens(ref.Snippet)
		if result.Body != "" {
			tokenEstimate = output.EstimateTokens(result.Body)
		}
	}

	// If summary mode, return condensed output
	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Results: []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    "refs",
				Query:      queryInfo,
				RepoRoot:   dir,
				IndexState: query.CheckIndexState(s.DB(), dir, Version),
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Offset:     off,
				Limit:      lim,
				Truncated:  len(results) >= lim,
			},
		}
		return w.WriteResponse(summaryResp)
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "refs",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Offset:        off,
			Limit:         lim,
			Truncated:     len(results) >= lim,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
