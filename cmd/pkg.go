package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var pkgDigest bool

func runPkg(args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	format := GetResponseFormat()

	// pkg is an orientation command — show full surface by default
	if !flagPassed("limit") {
		lim = 200
	}
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	pkgPattern := args[0]

	s, dir, err := OpenStore(w, cmdNamePkg)
	if err != nil {
		return err
	}
	defer s.Close()

	repoRoot, _ := s.GetMeta("repo_root")
	pkgPattern = query.ResolvePkgPattern(s.DB(), pkgPattern, dir, repoRoot)

	if pkgDigest {
		return runPkgDigest(s, dir, pkgPattern, start)
	}

	// Resolve full pkg path for doc lookup.
	fullPkgPath := query.FindFullPkgPath(s.DB(), pkgPattern)

	queryInfo := map[string]string{flagPackage: pkgPattern}

	// Find package symbols ranked by usage.
	symbols, err := query.FindPackageSymbolsByUsage(s.DB(), pkgPattern, lim, off)
	if err != nil {
		return w.WriteError(cmdNamePkg, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	if len(symbols) == 0 {
		return w.WriteError(cmdNamePkg, &output.Error{
			Code:    output.ErrNotFound,
			Message: "no exported symbols found in package matching: " + pkgPattern,
		})
	}

	// Convert to results.
	results := make([]output.Result, len(symbols))
	tokenEstimate := 0
	var degraded []string

	for i := range symbols {
		sym := &symbols[i]
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
	pkgDoc := query.GetPackageDoc(s.DB(), fullPkgPath)

	if summary {
		summaryData := output.BuildSummary(results)
		summaryResp := output.Response[output.Summary]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  []output.Summary{summaryData},
			Meta: output.Meta{
				Command:    cmdNamePkg,
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
				PkgDoc:     pkgDoc,
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
		fmt.Fprintf(os.Stdout, "files: %d  loc: %d  exports: %d\n", fileCount, loc, len(results))
		if headers := fileHeadersForDir(s.DB(), pkgDir, pkgDoc); len(headers) > 0 {
			for _, fh := range headers {
				fmt.Fprintf(os.Stdout, "  %s — %s\n", fh.Name, fh.Header)
			}
		}
		fmt.Fprintln(os.Stdout)
	}

	meta := output.Meta{
		Command:       cmdNamePkg,
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
		PkgDoc:        pkgDoc,
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

// pkgDigestRow is the JSON shape for 'snipe pkg <p> --digest' — a one-shot
// package-level metric digest (snipe-1o1). Designed so cohesion/arch linters
// in lintbrush get every datapoint they need in a single invocation, replacing
// the multi-command reconstruction they do today.
type pkgDigestRow struct {
	Pkg          string  `json:"pkg"`
	ExportsCount int     `json:"exports_count"`
	Ca           float64 `json:"ca"`
	Ce           float64 `json:"ce"`
	Instability  float64 `json:"instability"`
	LCOM4        float64 `json:"lcom4"`
	CycloMax     float64 `json:"cyclo_max"`
}

func runPkgDigest(s *store.Store, dir, pkgPattern string, startedAt time.Time) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	fullPkg := query.FindFullPkgPath(s.DB(), pkgPattern)
	if fullPkg == "" {
		fullPkg = pkgPattern
	}

	row := pkgDigestRow{Pkg: fullPkg}

	// exports_count: number of exported symbols in the package. LIMIT 0 returns
	// nothing in SQLite, so use a large sentinel — packages have far fewer.
	exports, err := query.FindPackageSymbolsByUsage(s.DB(), pkgPattern, 100000, 0)
	if err == nil {
		row.ExportsCount = len(exports)
	}

	// graph_metrics is keyed by package import path; pull each row-level metric.
	pull := func(metric string) float64 {
		rows, err := s.ReadTopN(cmdNameImports, metric, 0)
		if err != nil {
			return 0
		}
		for _, r := range rows {
			if r.NodeID == fullPkg {
				return r.Value
			}
		}
		return 0
	}
	row.Ca = pull("ca")
	row.Ce = pull("ce")
	row.Instability = pull("instability")
	row.LCOM4 = pull(kindLCOM4)
	row.CycloMax = pull("cyclo_max")

	resp := output.Response[pkgDigestRow]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []pkgDigestRow{row},
		Meta: output.Meta{
			Command:  cmdNamePkg,
			Query:    map[string]string{flagPackage: pkgPattern, "digest": "1"},
			RepoRoot: dir,
			Ms:       time.Since(startedAt).Milliseconds(),
			Total:    1,
		},
	}
	// --digest always emits JSON; the output is structured by intent.
	if GetOutputFormat() == output.OutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	// Default text rendering: short single-line digest for grep-friendly use.
	_, _ = fmt.Fprintf(os.Stdout,
		"pkg=%s exports=%d ca=%.0f ce=%.0f I=%.3f lcom4=%.0f cyclo_max=%.0f\n",
		row.Pkg, row.ExportsCount, row.Ca, row.Ce, row.Instability, row.LCOM4, row.CycloMax,
	)
	_ = w
	return nil
}
