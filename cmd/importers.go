package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var importersCmd = &cobra.Command{
	Use:   "importers <package>",
	Short: "Find files that import a package",
	Long: `Shows all files that import a given package.

Examples:
  snipe importers internal/handler    # Find importers of local package
  snipe importers encoding/json       # Find importers of stdlib package
  snipe importers github.com/foo/bar  # Find importers by full path`,
	Args: cobra.ExactArgs(1),
	RunE: runImporters,
}

func init() {
	rootCmd.AddCommand(importersCmd)
}

func runImporters(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, human, lim, offset, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	pkgPath := args[0]

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("importers", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if !store.Exists(dbPath) {
		return w.WriteError("importers", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("importers", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	// Use directory matching if it looks like a local path
	var imports []query.ImportRow
	if strings.Contains(pkgPath, "/") && !strings.Contains(pkgPath, ".") {
		// Looks like a local directory path
		imports, err = query.FindImportersByDirectory(s.DB(), pkgPath, lim, offset)
	} else {
		imports, err = query.FindImportsByPackage(s.DB(), pkgPath, lim, offset)
	}

	if err != nil {
		return w.WriteError("importers", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Convert to results - group by importing file
	results := make([]output.Result, len(imports))
	tokenEstimate := 0

	for i, imp := range imports {
		impRange := output.Range{
			Start: output.Position{Line: imp.Line, Col: imp.Col},
			End:   output.Position{Line: imp.Line, Col: imp.Col + len(imp.PkgPath)},
		}
		results[i] = output.Result{
			ID:         imp.FilePath + ":" + imp.PkgPath,
			File:       imp.FilePath,
			Range:      impRange,
			Kind:       "import",
			Name:       imp.PkgPath,
			Match:      "import \"" + imp.PkgPath + "\"",
			EditTarget: output.FormatEditTarget(imp.FilePath, impRange, ""),
		}
		tokenEstimate += output.EstimateTokens(imp.FilePath)
	}

	// Apply token budget truncation if specified
	maxTok := GetMaxTokens()
	tokenTruncated := false
	if maxTok > 0 {
		results, tokenTruncated = output.TruncateToTokenBudget(results, maxTok)
	}

	// Recalculate token estimate after truncation
	tokenEstimate = 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	resp := output.Response[output.Result]{
		Results: results,
		Meta: output.Meta{
			Command:       "importers",
			Query:         map[string]string{"package": pkgPath},
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Truncated:     len(results) >= lim || tokenTruncated,
			TokenEstimate: tokenEstimate,
			Offset:        offset,
			Limit:         lim,
		},
	}

	return w.WriteResponse(resp)
}
