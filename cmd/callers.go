package cmd

import (
	"encoding/hex"
	"os"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/query"
)

var callersID string

func runCallers(args []string) error {
	start := time.Now()

	_, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()

	// Apply format overrides
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary

	w := output.NewWriter(os.Stdout, GetOutputFormat())

	if len(args) == 0 && callersID == "" {
		return w.WriteError(cmdNameCallers, &protocol.Error{
			Code:    protocol.ErrInternal,
			Message: "provide a symbol name or --id",
		})
	}

	// Find repo root and open store (auto-indexes if needed)
	s, dir, err := OpenStore(w, cmdNameCallers)
	if err != nil {
		return err
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
			return w.WriteError(cmdNameCallers, &protocol.Error{
				Code:    protocol.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError(cmdNameCallers, protocol.NewNotFoundError(name))
		}

		if len(symbols) > 1 {
			candidates := make([]protocol.Candidate, len(symbols))
			for i := range symbols {
				sym := &symbols[i]
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError(cmdNameCallers, protocol.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{flagSymbol: name}
	}

findCallers:

	var symName string
	if sym, err := query.LookupByID(s.DB(), symbolID); err == nil && sym != nil {
		symName = sym.Name
	}

	// Find callers
	calls, err := query.FindCallers(s.DB(), symbolID, lim, off)
	if err != nil {
		return w.WriteError(cmdNameCallers, &protocol.Error{
			Code:    protocol.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - show the caller functions, deduplicated by caller ID
	results := make([]protocol.Result, 0, len(calls))
	tokenEstimate := 0
	var degraded []string

	// Batch fetch caller symbols if bodies are requested (avoids N+1 queries)
	var callerSymbols map[string]*query.SymbolRow
	if withBody && len(calls) > 0 {
		callerIDs := make([]string, len(calls))
		for i := range calls {
			call := &calls[i]
			callerIDs[i] = call.CallerID
		}
		var batchErr error
		callerSymbols, batchErr = query.BatchLookupByID(s.DB(), callerIDs)
		if batchErr != nil {
			degraded = append(degraded, "batch_lookup_failed")
		}
	}

	seen := make(map[string]bool, len(calls))
	for i := range calls {
		call := &calls[i]
		if seen[call.CallerID] {
			continue
		}
		seen[call.CallerID] = true

		result := call.ToCallerResult()

		// Add caller body if requested (from the caller's definition, not call site)
		if withBody {
			if callerSym, ok := callerSymbols[call.CallerID]; ok && callerSym != nil {
				callerResult := callerSym.ToResult()
				if err := protocol.AddBody(&callerResult); err != nil {
					degraded = append(degraded, "body_extraction_failed")
				}
				result.Body = callerResult.Body
			}
		}

		if contextLines > 0 && !withBody {
			if err := protocol.AddContext(&result, contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}

		results = append(results, result)
		tokenEstimate += protocol.EstimateTokens(call.CallerSignature.String)
		if result.Body != "" {
			tokenEstimate += protocol.EstimateTokens(result.Body)
		}
	}

	// Deduplicate degraded messages
	degraded = uniqueStrings(degraded)

	// Score, sort, and apply selection
	protocol.ScoreAndSort(results, symName)
	results = ApplySelection(results)

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = protocol.TruncateToTokenBudget(results, maxTok)
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	// If summary mode, return condensed output
	if summary {
		summaryData := protocol.BuildSummary(results)
		summaryResp := protocol.Response[protocol.Summary]{
			Protocol: protocol.ProtocolVersion,
			Ok:       true,
			Results:  []protocol.Summary{summaryData},
			Meta: protocol.Meta{
				Command:    cmdNameCallers,
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
		tokenEstimate += protocol.EstimateResultTokens(&results[i])
	}

	resp := protocol.Response[protocol.Result]{
		Protocol:    protocol.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: protocol.SuggestionsForCallers(symName, len(results)),
		Meta: protocol.Meta{
			Command:       cmdNameCallers,
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
