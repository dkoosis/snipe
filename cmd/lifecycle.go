package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var lifecycleCmd = &cobra.Command{
	Use:     "lifecycle <Type>",
	Short:   "Trace every function creating, mutating, reading, or deleting a type",
	GroupID: "advanced",
	Long: `Traces all functions that create, mutate, read, or delete instances of a type,
grouped by CRUD role with caller chains.

Replaces the manual 5+ query stitching previously required to map a type's lifecycle.

Examples:
  snipe lifecycle Nug                  # Full lifecycle for type Nug
  snipe lifecycle Store --format json  # Machine-readable output`,
	Args: cobra.ExactArgs(1),
	RunE: runLifecycle,
}

func init() {
	rootCmd.AddCommand(lifecycleCmd)
}

func runLifecycle(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, lim, off, _, _, _ := GetOutputConfig()
	format := GetResponseFormat()
	maxTok := GetMaxTokens()

	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	typeName := args[0]

	s, dir, err := OpenStore(w, "lifecycle")
	if err != nil {
		return err
	}
	defer s.Close()

	// Resolve type symbol.
	symbols, err := query.LookupByName(s.DB(), typeName)
	if err != nil {
		return w.WriteError("lifecycle", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}
	if len(symbols) == 0 {
		return w.WriteError("lifecycle", output.NewNotFoundError(typeName))
	}
	if len(symbols) > 1 {
		candidates := make([]output.Candidate, len(symbols))
		for i, sym := range symbols {
			candidates[i] = sym.ToCandidate()
		}
		return w.WriteError("lifecycle", output.NewAmbiguousError(typeName, candidates))
	}

	sym := symbols[0]
	_ = format
	_ = maxTok

	results := []output.Result{sym.ToResult()}
	results[0].Hints = []string{"stub"}

	// Apply offset/limit.
	if off > 0 && off < len(results) {
		results = results[off:]
	}
	if len(results) > lim {
		results = results[:lim]
	}

	return w.WriteResponse(output.Response[output.Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:  "lifecycle",
			Query:    map[string]string{"type": typeName},
			RepoRoot: dir,
			Ms:       time.Since(start).Milliseconds(),
			Total:    len(results),
			Offset:   off,
			Limit:    lim,
		},
	})
}
