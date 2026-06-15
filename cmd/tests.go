package cmd

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	testsDirect bool
	testsAt     string
	testsID     string
)

func runTests(args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary

	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	if len(args) == 0 && testsAt == "" && testsID == "" {
		return w.WriteError(cmdNameTests, &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name, --at position, or --id",
		})
	}

	s, dir, err := OpenStore(w, cmdNameTests)
	if err != nil {
		return err
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	switch {
	case testsID != "":
		symbolID = testsID
		queryInfo = map[string]string{"id": testsID}

	case testsAt != "":
		pos, err := query.ParsePosition(testsAt)
		if err != nil {
			return w.WriteError(cmdNameTests, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		// FindSymbolAtPosition expects a relative path (file_path_rel).
		// Make absolute paths relative to repo root.
		filePath := pos.File
		if filepath.IsAbs(filePath) {
			if rel, err := filepath.Rel(dir, filePath); err == nil {
				filePath = rel
			}
		}
		sym := query.FindSymbolAtPosition(s.DB(), filePath, pos.Line)
		if sym == nil {
			return w.WriteError(cmdNameTests, &output.Error{
				Code:    output.ErrNotFound,
				Message: "no symbol found at " + testsAt,
			})
		}
		symbolID = sym.ID
		queryInfo = map[string]string{"at": testsAt, "resolved": sym.Name}

	default:
		name := args[0]

		// Auto-detect hex ID
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				break
			}
		}

		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameTests, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		if len(symbols) == 0 {
			return w.WriteError(cmdNameTests, output.NewNotFoundError(name))
		}
		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i := range symbols {
				sym := &symbols[i]
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError(cmdNameTests, output.NewAmbiguousError(name, candidates))
		}
		symbolID = symbols[0].ID
		queryInfo = map[string]string{flagSymbol: name}
	}

	// Look up symbol for session tracking and suggestions
	var symName, symFileRel string
	if sym, err := query.LookupByID(s.DB(), symbolID); err == nil && sym != nil {
		symName = sym.Name
		symFileRel = sym.FilePathRel
		recordSessionQuery(dir, sym.Name, sym.FilePathRel, sym.LineStart, sym.Kind, cmdNameTests)
	}

	// Find tests
	testRows, err := query.FindTests(s.DB(), symbolID, testsDirect, lim, off)
	if err != nil {
		return w.WriteError(cmdNameTests, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results with hints
	results := make([]output.Result, len(testRows))
	var degraded []string

	// Batch fetch for bodies
	var testSymbols map[string]*query.SymbolRow
	if withBody && len(testRows) > 0 {
		ids := make([]string, len(testRows))
		for i := range testRows {
			tr := &testRows[i]
			ids[i] = tr.ID
		}
		var batchErr error
		testSymbols, batchErr = query.BatchLookupByID(s.DB(), ids)
		if batchErr != nil {
			degraded = append(degraded, "batch_lookup_failed")
		}
	}

	for i := range testRows {
		tr := &testRows[i]
		result := tr.ToResult()

		// Add hop hint
		if tr.Hop == 1 {
			result.Hints = append(result.Hints, "direct_test")
		} else {
			result.Hints = append(result.Hints, "transitive_test")
		}

		// Add body if requested
		if withBody {
			if sym, ok := testSymbols[tr.ID]; ok && sym != nil {
				symResult := sym.ToResult()
				if err := output.AddBody(&symResult); err != nil {
					degraded = append(degraded, "body_extraction_failed")
				}
				result.Body = symResult.Body
			}
		}

		if contextLines > 0 && !withBody {
			if err := output.AddContext(&result, contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}

		results[i] = result
	}

	degraded = uniqueStrings(degraded)

	output.ScoreAndSort(results, symName)
	results = ApplySelection(results)

	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	if summary {
		summaryData := output.BuildSummary(results)
		return w.WriteResponse(output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    cmdNameTests,
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
		})
	}

	// Token estimate
	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	// Suggested test file for zero-coverage
	suggestedFile := ""
	if symFileRel != "" {
		suggestedFile = strings.TrimSuffix(symFileRel, ".go") + "_test.go"
	}

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     results,
		Suggestions: output.SuggestionsForTests(symName, len(results), suggestedFile),
		Meta: output.Meta{
			Command:       cmdNameTests,
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
