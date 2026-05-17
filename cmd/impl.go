package cmd

import (
	"encoding/hex"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var implCmd = &cobra.Command{
	Use:     "impl INTERFACE",
	Short:   "Find types implementing an interface",
	GroupID: categoryNavigate,
	Long: `Finds types that potentially implement a given interface.

Since Go uses structural typing, this command finds types that reference
the interface in the same file, which often indicates implementation.

Examples:
  snipe impl Reader              # Find types implementing Reader
  snipe impl --id abc123         # Find implementers by interface ID
  snipe impl io.Writer           # Qualified interface name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImpl,
}

var implID string

func init() {
	implCmd.Flags().StringVar(&implID, "id", "", "Interface ID to look up")
	rootCmd.AddCommand(implCmd)
}

func runImpl(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	if len(args) == 0 && implID == "" {
		return w.WriteError(cmdNameImpl, &output.Error{
			Code:    output.ErrInternal,
			Message: "provide an interface name or --id",
		})
	}

	s, dir, err := OpenStore(w, cmdNameImpl)
	if err != nil {
		return err
	}
	defer s.Close()

	var interfaceID string
	var queryInfo map[string]string

	if implID != "" {
		interfaceID = implID
		queryInfo = map[string]string{"id": implID}
	} else {
		name := args[0]

		// Check if input looks like a symbol ID (16-char hex string)
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				interfaceID = name
				queryInfo = map[string]string{"id": name}
				goto findImplementers
			}
		}

		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameImpl, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError(cmdNameImpl, output.NewNotFoundError(name))
		}

		// Filter to interfaces only
		var interfaces []query.SymbolRow
		for i := range symbols {
			sym := &symbols[i]
			if sym.Kind == cmdKindInterface {
				interfaces = append(interfaces, *sym)
			}
		}

		if len(interfaces) == 0 {
			return w.WriteError(cmdNameImpl, &output.Error{
				Code:    output.ErrNotFound,
				Message: name + " is not an interface",
				Suggestions: []output.Suggestion{
					{Command: "snipe def " + name, Description: "View definition of " + name, Priority: 1},
					{Command: "snipe refs " + name, Description: "Find references to " + name, Priority: 2},
				},
			})
		}

		if len(interfaces) > 1 {
			candidates := make([]output.Candidate, len(interfaces))
			for i := range interfaces {
				sym := &interfaces[i]
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError(cmdNameImpl, output.NewAmbiguousError(name, candidates))
		}

		interfaceID = interfaces[0].ID
		queryInfo = map[string]string{cmdKindInterface: name}
	}

findImplementers:
	// Find implementers
	implementers, err := query.FindImplementers(s.DB(), interfaceID, lim, off)
	if err != nil {
		return w.WriteError(cmdNameImpl, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results
	results := make([]output.Result, len(implementers))
	tokenEstimate := 0
	var degraded []string

	for i := range implementers {
		impl := &implementers[i]
		result := impl.ToResult()

		// Add body if requested
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

		results[i] = result
		tokenEstimate += output.EstimateTokens(impl.Signature.String)
		if result.Body != "" {
			tokenEstimate += output.EstimateTokens(result.Body)
		}
	}

	// Deduplicate degraded messages
	degraded = uniqueStrings(degraded)

	// Score, sort, and apply selection
	var queryName string
	if implID != "" {
		queryName = implID
	} else if len(args) > 0 {
		queryName = args[0]
	}
	output.ScoreAndSort(results, queryName)
	results = ApplySelection(results)

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	// If summary mode, return condensed output
	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    cmdNameImpl,
				Query:      queryInfo,
				RepoRoot:   dir,
				IndexState: query.CheckIndexState(s.DB(), dir, Version),
				Degraded:   degraded,
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Offset:     off,
				Limit:      lim,
				Truncated:  len(results) >= lim,
				StaleFiles: staleFiles,
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
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:       cmdNameImpl,
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
			StaleFiles:    staleFiles,
		},
	}

	return w.WriteResponse(resp)
}
