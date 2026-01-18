package cmd

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/kg"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	defAt string
)

var defCmd = &cobra.Command{
	Use:   "def [symbol]",
	Short: "Jump to symbol definition",
	Long: `Finds the definition of a symbol by name or position.

Examples:
  snipe def ProcessOrder           # Find by name
  snipe def --at main.go:42:12     # Find at position
  snipe def pkg/handler.Handler    # Qualified name
  snipe def "(*Server).Start"      # Method syntax`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDef,
}

func init() {
	defCmd.Flags().StringVar(&defAt, "at", "", "Position to look up (file:line:col)")
	rootCmd.AddCommand(defCmd)
}

func runDef(cmd *cobra.Command, args []string) error {
	start := time.Now()

	human, compact, _, _, contextLines, withBody, withSiblings := GetOutputConfig()
	format := GetResponseFormat()

	// Apply format overrides
	withBody, withSiblings, contextLines = ApplyFormatOverrides(format, withBody, withSiblings, contextLines)

	w := output.NewWriter(os.Stdout, human, compact)

	// Need either a symbol name or --at position
	if len(args) == 0 && defAt == "" {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name or --at position",
		})
	}

	// Find repo root and open store (auto-indexes if needed)
	s, dir, err := OpenStore(w, "def")
	if err != nil {
		return err // Error already written by OpenStore
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string
	var decisionPath []string

	if defAt != "" {
		// Resolve position
		pos, err := query.ParsePosition(defAt)
		if err != nil {
			return w.WriteError("def", &output.Error{
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
			return w.WriteError("def", &output.Error{
				Code:    output.ErrNotFound,
				Message: err.Error(),
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
					return w.WriteError("def", &output.Error{
						Code:    output.ErrInternal,
						Message: err.Error(),
					})
				}
				if len(symbols) == 1 {
					symbolID = symbols[0].ID
					queryInfo = map[string]string{"symbol": symbolPart, "file": filePart}
					decisionPath = append(decisionPath, "lookup:file_qualified")
					goto lookup
				} else if len(symbols) > 1 {
					candidates := make([]output.Candidate, len(symbols))
					for i, s := range symbols {
						candidates[i] = s.ToCandidate()
					}
					return w.WriteError("def", output.NewAmbiguousError(name, candidates))
				}
				// Fall through to regular lookup if not found
			}
		}

		// Look up by name
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("def", &output.Error{
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
				return w.WriteError("def", output.NewNotFoundError(name))
			}
			return w.WriteError("def", output.NewNotFoundError(name, suggestions...))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, s := range symbols {
				candidates[i] = s.ToCandidate()
			}
			return w.WriteError("def", output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{"symbol": name}
		decisionPath = append(decisionPath, "lookup:name")
	}

lookup:
	// Get the symbol details
	sym, err := query.LookupByID(s.DB(), symbolID)
	if err != nil {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if sym == nil {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrNotFound,
			Message: fmt.Sprintf("symbol %s not found", symbolID),
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

	// Add callers_preview for func/method kinds (always include top 3)
	if sym.Kind == "func" || sym.Kind == "method" {
		callers, err := query.GetCallersPreview(s.DB(), sym.ID, 3)
		if err != nil {
			degraded = append(degraded, "callers_preview_failed")
		} else if len(callers) > 0 {
			result.CallersPreview = callers
		}
	}

	// Add KG hints if requested
	if GetWithKGHints() {
		hints := kg.GetHints(kg.Config{
			File:    sym.FilePathRel,
			Symbol:  sym.Name,
			Package: sym.PkgPath,
		})
		if len(hints) > 0 {
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

	tokenEstimate := output.EstimateTokens(result.Match)
	if result.Body != "" {
		tokenEstimate = output.EstimateTokens(result.Body)
	}

	resp := output.Response[output.Result]{
		Results: []output.Result{result},
		Meta: output.Meta{
			Command:       "def",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         1,
			TokenEstimate: tokenEstimate,
			DecisionPath:  decisionPath,
		},
	}

	return w.WriteResponse(resp)
}
