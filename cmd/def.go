package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
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

	_, human, _, contextLines, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	// Need either a symbol name or --at position
	if len(args) == 0 && defAt == "" {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a symbol name or --at position",
		})
	}

	// Find repo root and open store
	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	dbPath := store.DefaultIndexPath(dir)
	if !store.Exists(dbPath) {
		return w.WriteError("def", output.NewMissingIndexError())
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("def", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to open index: " + err.Error(),
		})
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

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
	} else {
		// Look up by name
		name := args[0]
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError("def", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError("def", output.NewNotFoundError(name))
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
	}

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

	result := sym.ToResult()

	// Add context lines if requested
	if contextLines > 0 {
		_ = output.AddContext(&result, contextLines)
	}

	tokenEstimate := output.EstimateTokens(result.Match)

	resp := output.Response[output.Result]{
		Results: []output.Result{result},
		Meta: output.Meta{
			Command:       "def",
			Query:         queryInfo,
			RepoRoot:      dir,
			IndexState:    query.CheckIndexState(s.DB(), dir, Version),
			Ms:            time.Since(start).Milliseconds(),
			Total:         1,
			TokenEstimate: tokenEstimate,
		},
	}

	return w.WriteResponse(resp)
}
