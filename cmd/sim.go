package cmd

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/embed"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/vector"
)

var simCmd = &cobra.Command{
	Use:     "sim [query]",
	Short:   "Semantic similarity search",
	GroupID: categoryEmbed,
	Long: `Finds symbols semantically similar to the query using embeddings.

Requires embeddings to be generated first with 'snipe index --embed'.

Examples:
  snipe sim "handle HTTP request"
  snipe sim "database connection pool"
  snipe sim --threshold 0.5 "error handling"
  snipe sim --within-pkg --pairs --threshold=0.9   # near-dup pairs inside each package
  snipe sim --within-pkg --pairs --shared-callees  # also count shared callees`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSim,
}

var (
	simThreshold     float64
	simWithinPkg     bool
	simPairs         bool
	simSharedCallees bool
)

func init() {
	simCmd.Flags().Float64Var(&simThreshold, "threshold", 0.3, "Minimum similarity threshold (0-1)")
	simCmd.Flags().BoolVar(&simWithinPkg, "within-pkg", false, "Restrict pair scan to symbols in the same package (with --pairs)")
	simCmd.Flags().BoolVar(&simPairs, "pairs", false, "Emit near-duplicate symbol pairs (JSONL) instead of running a query")
	simCmd.Flags().BoolVar(&simSharedCallees, "shared-callees", false, "Include shared-callee counts on each pair (with --pairs)")
	rootCmd.AddCommand(simCmd)
}

func runSim(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, lim, off, contextLines, withBody, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	if simPairs {
		s, dir, err := OpenStore(w, cmdNameSim)
		if err != nil {
			return err
		}
		defer s.Close()
		return runSimPairs(s, dir, start)
	}

	if len(args) == 0 {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: "sim requires a query argument unless --pairs is set",
		})
	}
	return runSimQuery(cmd, args, start, compact, lim, off, contextLines, withBody)
}

func runSimQuery(cmd *cobra.Command, args []string, start time.Time, compact bool, lim, off, contextLines int, withBody bool) error {
	queryText := args[0]
	format := GetResponseFormat()
	withBody, _, contextLines = ApplyFormatOverrides(format, withBody, false, contextLines)
	summary := format == FormatSummary
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, cmdNameSim)
	if err != nil {
		return err
	}
	defer s.Close()

	// Get embedding client
	client, err := embed.NewClient()
	if err != nil {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: "embedding client: " + err.Error(),
		})
	}

	// Run semantic search (fetch off+lim to support offset)
	threshold := float32(simThreshold)
	searchLimit := off + lim
	results, _, simErr := embed.Search(cmd.Context(), queryText, s, client, searchLimit, threshold)
	if simErr != nil {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: simErr.Error(),
		})
	}
	if results == nil {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: "no embeddings found. Run 'snipe index --embed' first",
		})
	}

	// Apply offset
	totalBeforeOffset := len(results)
	if off > 0 && off < len(results) {
		results = results[off:]
	} else if off >= len(results) {
		results = nil
	}
	if len(results) > lim {
		results = results[:lim]
	}

	// Add body/context and track degraded
	var degraded []string
	for i := range results {
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

	// Apply token budget truncation if specified
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
				Command:    cmdNameSim,
				Query:      map[string]string{"query": queryText, "threshold": cmd.Flag("threshold").Value.String()},
				RepoRoot:   dir,
				IndexState: query.CheckIndexState(s.DB(), dir, Version),
				Degraded:   degraded,
				Ms:         time.Since(start).Milliseconds(),
				Total:      summaryData.Total,
				Offset:     off,
				Limit:      lim,
				Truncated:  totalBeforeOffset >= searchLimit,
				StaleFiles: staleFiles,
			},
		}
		return w.WriteResponse(summaryResp)
	}

	// Calculate token estimate after truncation
	tokenEstimate := 0
	for i := range results {
		tokenEstimate += output.EstimateResultTokens(&results[i])
	}

	resp := output.Response[output.Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:       cmdNameSim,
			Query:         map[string]string{"query": queryText, "threshold": cmd.Flag("threshold").Value.String()},
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Degraded:      degraded,
			Ms:            time.Since(start).Milliseconds(),
			Total:         len(results),
			Offset:        off,
			Limit:         lim,
			Truncated:     totalBeforeOffset >= searchLimit || tokenTruncated,
			TokenEstimate: tokenEstimate,
			StaleFiles:    staleFiles,
		},
	}

	return w.WriteResponse(resp)
}

// simPairRow is one near-duplicate symbol pair within a package (snipe-3jb).
type simPairRow struct {
	Pkg           string  `json:"pkg"`
	SymA          string  `json:"sym_a"`
	SymB          string  `json:"sym_b"`
	IDA           string  `json:"id_a"`
	IDB           string  `json:"id_b"`
	Similarity    float32 `json:"similarity"`
	SharedCallees int     `json:"shared_callees,omitempty"`
	TotalCalleesA int     `json:"total_callees_a,omitempty"`
	TotalCalleesB int     `json:"total_callees_b,omitempty"`
}

func runSimPairs(s *store.Store, dir string, startedAt time.Time) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())
	if !simWithinPkg {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: "--pairs currently requires --within-pkg",
		})
	}

	rows, err := s.GetEmbeddingsByPackage()
	if err != nil {
		return w.WriteError(cmdNameSim, &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}
	if len(rows) == 0 {
		return w.WriteError(cmdNameSim, &output.Error{
			Code:    output.ErrInternal,
			Message: "no embeddings — run 'snipe index --embed' first",
		})
	}

	byPkg := make(map[string][]store.PkgEmbeddingRow)
	for i := range rows {
		r := &rows[i]
		byPkg[r.Pkg] = append(byPkg[r.Pkg], *r)
	}

	threshold := float32(simThreshold)
	var pairs []simPairRow
	for pkg, syms := range byPkg {
		for i := 0; i < len(syms); i++ {
			for j := i + 1; j < len(syms); j++ {
				sim := vector.CosineSimilarity(syms[i].Embedding, syms[j].Embedding)
				if sim < threshold {
					continue
				}
				a, b := syms[i], syms[j]
				if a.Name > b.Name {
					a, b = b, a
				}
				pairs = append(pairs, simPairRow{
					Pkg:        pkg,
					SymA:       a.Name,
					SymB:       b.Name,
					IDA:        a.SymbolID,
					IDB:        b.SymbolID,
					Similarity: sim,
				})
			}
		}
	}

	if simSharedCallees && len(pairs) > 0 {
		ids := make([]string, 0, len(pairs)*2)
		seen := make(map[string]bool)
		for _, p := range pairs {
			for _, id := range []string{p.IDA, p.IDB} {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		callees, err := s.GetCalleesForSymbols(ids)
		if err == nil {
			for i := range pairs {
				p := &pairs[i]
				a := callees[p.IDA]
				b := callees[p.IDB]
				shared := 0
				for id := range a {
					if _, ok := b[id]; ok {
						shared++
					}
				}
				p.SharedCallees = shared
				p.TotalCalleesA = len(a)
				p.TotalCalleesB = len(b)
			}
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Pkg != pairs[j].Pkg {
			return pairs[i].Pkg < pairs[j].Pkg
		}
		if pairs[i].Similarity != pairs[j].Similarity {
			return pairs[i].Similarity > pairs[j].Similarity
		}
		return pairs[i].SymA < pairs[j].SymA
	})

	enc := json.NewEncoder(os.Stdout)
	for _, p := range pairs {
		if err := enc.Encode(p); err != nil {
			return err
		}
	}
	_ = dir
	_ = startedAt
	return nil
}
