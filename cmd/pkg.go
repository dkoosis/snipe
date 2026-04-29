package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var pkgCmd = &cobra.Command{
	Use:     "pkg <name>",
	Short:   "Show package overview with exported symbols",
	GroupID: "advanced",
	Long: `Shows an overview of a package including its exported symbols.

Displays all exported types, functions, constants, and variables in a package,
ranked by usage (most-referenced symbols first) so the important ones surface first.

Examples:
  snipe pkg store              # Show exported symbols in store package
  snipe pkg internal/query     # Show exported symbols in internal/query
  snipe pkg output             # Show exported symbols matching 'output'`,
	Args: cobra.ExactArgs(1),
	RunE: runPkg,
}

func init() {
	rootCmd.AddCommand(pkgCmd)
}

func runPkg(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()

	// pkg is an orientation command — show full surface by default
	if !cmd.Flags().Changed("limit") {
		lim = 200
	}
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	pkgPattern := args[0]

	s, dir, err := OpenStore(w, "pkg")
	if err != nil {
		return err
	}
	defer s.Close()

	repoRoot, _ := s.GetMeta("repo_root")
	pkgPattern = query.ResolvePkgPattern(s.DB(), pkgPattern, dir, repoRoot)

	// Resolve full pkg path for doc lookup.
	fullPkgPath := query.FindFullPkgPath(s.DB(), pkgPattern)

	queryInfo := map[string]string{"package": pkgPattern}

	// Find package symbols ranked by usage.
	symbols, err := query.FindPackageSymbolsByUsage(s.DB(), pkgPattern, lim, off)
	if err != nil {
		return w.WriteError("pkg", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if len(symbols) == 0 {
		return w.WriteError("pkg", &output.Error{
			Code:    output.ErrNotFound,
			Message: "no exported symbols found in package matching: " + pkgPattern,
		})
	}

	// Convert to results.
	results := make([]output.Result, len(symbols))
	tokenEstimate := 0
	var degraded []string

	for i, sym := range symbols {
		result := sym.ToResult()

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

		results[i] = result
		tokenEstimate += output.EstimateTokens(sym.Signature.String)
		if result.Body != "" {
			tokenEstimate += output.EstimateTokens(result.Body)
		}
	}

	degraded = uniqueStrings(degraded)

	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	staleFiles := query.CheckFileStaleness(s.DB(), dir, results)

	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    "pkg",
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

	// Recalculate token estimate after truncation.
	tokenEstimate = 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	// Write a package header before the symbol listing (Claude text mode only).
	if GetOutputFormat() != output.OutputJSON {
		pkgDoc := query.GetPackageDoc(s.DB(), fullPkgPath)
		pkgDir := pkgDirFromSymbols(s.DB(), fullPkgPath)
		fileCount, loc, _ := computePackageStats(s.DB(), fullPkgPath, pkgDir)
		displayPkg := pkgPattern
		if mod := query.DetectModulePath(s.DB()); mod != "" {
			if trim := strings.TrimPrefix(pkgPattern, mod); trim != pkgPattern {
				displayPkg = strings.TrimPrefix(trim, "/")
			}
		}
		fmt.Fprintf(os.Stdout, "# package %s\n", displayPkg)
		if pkgDoc != "" {
			fmt.Fprintf(os.Stdout, "%s\n", pkgDoc)
		}
		fmt.Fprintf(os.Stdout, "files: %d  loc: %d  exports: %d\n\n", fileCount, loc, len(results))
	}

	meta := output.Meta{
		Command:       "pkg",
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
	}

	if GetOutputFormat() != output.OutputJSON {
		w.WriteClaudePkgGrouped(results, meta)
		return nil
	}

	resp := output.Response[output.Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta:     meta,
	}
	return w.WriteResponse(resp)
}
