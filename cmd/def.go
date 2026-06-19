package cmd

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ctxpkg "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/kg"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	defAt   string
	defFile string
	defPkg  string
)

func runDef(args []string) error {
	start := time.Now()

	compact, _, _, contextLines, withBody, withSiblings := GetOutputConfig()
	format := GetResponseFormat()

	// Apply format overrides; def always shows body unless --no-body is explicit.
	_, withSiblings, contextLines = ApplyFormatOverrides(format, withBody, withSiblings, contextLines)

	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Handle --file and --pkg scoped queries.
	// --pkg + symbol name: look up the named symbol within the package.
	// --pkg alone (or --file): list all symbols in scope.
	if defPkg != "" && len(args) == 1 {
		return runDefInPkg(w, start, args[0], withBody, contextLines)
	}
	if defFile != "" || defPkg != "" {
		return runDefScoped(w, start, withBody, contextLines)
	}

	// Need either a symbol name or --at position
	if len(args) == 0 && defAt == "" {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrInternal,
			Message: errProvideSymbolOrAt,
		})
	}

	// Find repo root and open store (auto-indexes if needed)
	s, dir, err := OpenStore(w, cmdNameDef)
	if err != nil {
		return err // Error already written by OpenStore
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string
	var decisionPath []string
	var matchTier query.MatchTier

	if defAt != "" {
		// Resolve position
		pos, err := query.ParsePosition(defAt)
		if err != nil {
			return w.WriteError(cmdNameDef, &output.Error{
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
			return w.WriteError(cmdNameDef, &output.Error{
				Code:    output.ErrNotFound,
				Message: "no symbol found at " + defAt,
			})
		}
		queryInfo = map[string]string{"at": defAt}
		decisionPath = append(decisionPath, "lookup:position")
	} else {
		// Check if input looks like a symbol ID (16-char hex string)
		name := args[0]
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				// Input is a valid hex ID, look up directly
				symbolID = name
				queryInfo = map[string]string{"id": name}
				decisionPath = append(decisionPath, "lookup:id")
				goto lookup
			}
		}

		// Check for file-qualified syntax: file.go:SymbolName
		if idx := strings.LastIndex(name, ":"); idx > 0 && !strings.Contains(name[idx:], "/") {
			filePart := name[:idx]
			symbolPart := name[idx+1:]
			if symbolPart != "" && !strings.Contains(symbolPart, ":") {
				// This looks like file:symbol syntax
				symbols, err := query.LookupByNameInFile(s.DB(), symbolPart, filePart)
				if err != nil {
					return w.WriteError(cmdNameDef, &output.Error{
						Code:    output.ErrInternal,
						Message: err.Error(),
					})
				}
				if len(symbols) == 1 {
					symbolID = symbols[0].ID
					queryInfo = map[string]string{flagSymbol: symbolPart, flagFile: filePart}
					decisionPath = append(decisionPath, "lookup:file_qualified")
					goto lookup
				} else if len(symbols) > 1 {
					candidates := make([]output.Candidate, len(symbols))
					for i := range symbols {
						s := &symbols[i]
						candidates[i] = s.ToCandidate()
					}
					return w.WriteError(cmdNameDef, output.NewAmbiguousError(name, candidates))
				}
				// Fall through to regular lookup if not found
			}
		}

		// Look up by name
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameDef, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			// Try to find similar symbols for helpful suggestions
			maxDist := query.DefaultMaxDistance(name)
			suggestions, err := query.FindSimilarSymbols(s.DB(), name, maxDist, 3)
			if err != nil {
				// If fuzzy search fails, just return the basic error
				return w.WriteError(cmdNameDef, output.NewNotFoundError(name))
			}
			return w.WriteError(cmdNameDef, output.NewNotFoundError(name, suggestions...))
		}

		if len(symbols) > 1 {
			// Ambiguity fallback: if --select narrows to one/few, pick the
			// highest-scored candidate(s) instead of raising an error.
			// Satisfies D2: "one command should just work".
			if picked, ok := pickSelectedSymbol(symbols, name); ok {
				symbolID = picked.ID
				matchTier = picked.MatchTier
				queryInfo = map[string]string{flagSymbol: name}
				decisionPath = append(decisionPath, "lookup:name_select")
				goto lookup
			}
			candidates := make([]output.Candidate, len(symbols))
			for i := range symbols {
				s := &symbols[i]
				candidates[i] = s.ToCandidate()
			}
			return w.WriteError(cmdNameDef, output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		matchTier = symbols[0].MatchTier
		queryInfo = map[string]string{flagSymbol: name}
		decisionPath = append(decisionPath, "lookup:name")
	}

lookup:
	// Get the symbol details
	sym, err := query.LookupByID(s.DB(), symbolID)
	if err != nil {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if sym == nil {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrNotFound,
			Message: fmt.Sprintf("symbol %s not found", symbolID),
		})
	}

	result := sym.ToResultWithHints(s.DB())
	var degraded []string

	// Self-assessment: if the name resolved via a degraded fallback rung rather
	// than an exact match, emit the tier marker so ferret can tell a served
	// query from a substituted one (snipe-ffj). Silence = served.
	switch matchTier {
	case query.MatchExact:
		// Served: the literal query matched. Silence — no marker (D4).
	case query.MatchCaseInsens:
		degraded = append(degraded, output.DegradedCIMatch)
	}

	// Record query in session for active work tracking
	recordSessionQuery(dir, sym.Name, sym.FilePathRel, sym.LineStart, sym.Kind, cmdNameDef)

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

	// Add role classification in detailed format
	if format == FormatDetailed {
		role := ctxpkg.InferRoleForSymbol(s.DB(), sym.ID, sym.Name, sym.Kind,
			sym.Signature.String, sym.PkgPath, sym.FilePath)
		result.Role = string(role)
	}

	// Add callers_preview for func/method kinds (always include top 3)
	if sym.Kind == cmdKindFunc || sym.Kind == "method" {
		callers, err := query.GetCallersPreview(s.DB(), sym.ID, 3)
		if err != nil {
			degraded = append(degraded, "callers_preview_failed")
		} else if len(callers) > 0 {
			result.CallersPreview = callers
		}
	}

	// Add KG hints if requested. When the user explicitly opts in with
	// --kg-hints, surface whether the KG bridge was actually reachable —
	// otherwise the flag silently does nothing, which was the F7 complaint.
	if GetWithKGHints() {
		if !kg.IsAvailable() {
			degraded = append(degraded, "kg_unavailable")
		} else {
			hints := kg.GetHints(GetContext(), kg.Config{
				File:    sym.FilePathRel,
				Symbol:  sym.Name,
				Package: sym.PkgPath,
			})
			if len(hints) == 0 {
				degraded = append(degraded, "kg_no_hints")
			} else {
				result.KGHints = make([]output.KGHint, len(hints))
				for i, h := range hints {
					result.KGHints[i] = output.KGHint{
						ID:       h.ID,
						Kind:     h.Kind,
						Severity: h.Severity,
						Summary:  h.Summary,
					}
				}
			}
		}
	}

	tokenEstimate := output.EstimateTokens(result.Match)
	if result.Body != "" {
		tokenEstimate = output.EstimateTokens(result.Body)
	}

	results := []output.Result{result}

	// Apply score-based selection if specified
	results = ApplySelection(results)

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
		// Recalculate token estimate after truncation
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
			Command:       cmdNameDef,
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			TokenEstimate: tokenEstimate,
			DecisionPath:  decisionPath,
			StaleFiles:    staleFiles,
			Truncated:     tokenTruncated,
		},
	}

	return w.WriteResponse(resp)
}

// runDefInPkg handles `def --pkg <pkg> <name>`: find a named symbol within a package.
// Returns the symbol if unique, candidates if ambiguous, or not-found with suggestions.
func runDefInPkg(w *output.Writer, start time.Time, name string, withBody bool, contextLines int) error {
	s, dir, err := OpenStore(w, cmdNameDef)
	if err != nil {
		return err
	}
	defer s.Close()

	symbols, err := query.LookupByNameInPkg(s.DB(), name, defPkg)
	if err != nil {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if len(symbols) == 0 {
		pkgSyms, _ := query.FindPackageSymbols(s.DB(), defPkg, 10, 0)
		names := make([]string, len(pkgSyms))
		for i := range pkgSyms {
			sym := &pkgSyms[i]
			names[i] = sym.Name
		}
		return w.WriteError(cmdNameDef, output.NewNotFoundError(name+" in pkg "+defPkg, names...))
	}

	if len(symbols) > 1 {
		candidates := make([]output.Candidate, len(symbols))
		for i := range symbols {
			sym := &symbols[i]
			candidates[i] = sym.ToCandidate()
		}
		return w.WriteError(cmdNameDef, output.NewAmbiguousError(name, candidates))
	}

	sym := &symbols[0]
	result := sym.ToResultWithHints(s.DB())
	var degraded []string

	recordSessionQuery(dir, sym.Name, sym.FilePathRel, sym.LineStart, sym.Kind, cmdNameDef)

	if withBody {
		if err := output.AddBody(&result); err != nil {
			degraded = append(degraded, "body_extraction_failed")
		}
	}
	if contextLines > 0 && !withBody {
		if err := output.AddContext(&result, contextLines); err != nil {
			degraded = append(degraded, "context_extraction_failed")
		}
	}

	tokenEstimate := output.EstimateTokens(result.Match)
	if result.Body != "" {
		tokenEstimate = output.EstimateTokens(result.Body)
	}

	resp := output.Response[output.Result]{
		Protocol:    output.ProtocolVersion,
		Ok:          true,
		Results:     []output.Result{result},
		Suggestions: output.SuggestionsForDef(&result),
		Meta: output.Meta{
			Command:       cmdNameDef,
			Query:         map[string]string{flagSymbol: name, flagPkg: defPkg},
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         1,
			TokenEstimate: tokenEstimate,
			DecisionPath:  []string{"lookup:pkg_scoped"},
		},
	}

	return w.WriteResponse(resp)
}

// runDefScoped handles --file and --pkg scoped queries.
func runDefScoped(w *output.Writer, start time.Time, withBody bool, contextLines int) error {
	_, lim, off, _, _, _ := GetOutputConfig()

	s, dir, err := OpenStore(w, cmdNameDef)
	if err != nil {
		return err
	}
	defer s.Close()

	var symbols []query.SymbolRow
	var queryInfo map[string]string

	if defFile != "" && defPkg != "" {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrInternal,
			Message: "--file and --pkg are mutually exclusive",
		})
	}

	if defFile != "" {
		symbols, err = query.FindSymbolsInFile(s.DB(), defFile, lim, off)
		queryInfo = map[string]string{flagFile: defFile}
	} else {
		symbols, err = query.FindPackageSymbols(s.DB(), defPkg, lim, off)
		queryInfo = map[string]string{flagPkg: defPkg}
	}

	if err != nil {
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if len(symbols) == 0 {
		scope := defFile
		if scope == "" {
			scope = defPkg
		}
		return w.WriteError(cmdNameDef, &output.Error{
			Code:    output.ErrNotFound,
			Message: "no symbols found in " + scope,
		})
	}

	// Convert to results
	results := make([]output.Result, len(symbols))
	var degraded []string
	for i := range symbols {
		sym := &symbols[i]
		results[i] = sym.ToResult()
		if withBody {
			if err := output.AddBody(&results[i]); err != nil {
				degraded = append(degraded, "body_extraction_failed")
			}
		}
		if contextLines > 0 && !withBody {
			if err := output.AddContext(&results[i], contextLines); err != nil {
				degraded = append(degraded, "context_extraction_failed")
			}
		}
	}
	degraded = uniqueStrings(degraded)

	// Score, sort, and apply selection
	scope := defFile
	if scope == "" {
		scope = defPkg
	}
	output.ScoreAndSort(results, scope)
	results = ApplySelection(results)

	// Apply token budget truncation
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	resp := output.Response[output.Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:       cmdNameDef,
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Offset:        off,
			Limit:         lim,
			TokenEstimate: tokenEstimate,
			StaleFiles:    staleFiles,
			Truncated:     tokenTruncated,
		},
	}

	return w.WriteResponse(resp)
}
