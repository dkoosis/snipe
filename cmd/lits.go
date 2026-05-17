package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var litsCmd = &cobra.Command{
	Use:     "lits <value>",
	Short:   "Find all locations of a string literal or env var name",
	GroupID: categoryFind,
	Long: `Finds all call sites where a string literal appears in source — env var lookups
(os.Getenv, os.LookupEnv) and named string constants.

Examples:
  snipe lits SNIPE_VOYAGE_API_KEY   # Find all os.Getenv("SNIPE_VOYAGE_API_KEY") calls
  snipe lits "my constant value"    # Find string constant with this value`,
	Args: cobra.ExactArgs(1),
	RunE: runLits,
}

func init() {
	rootCmd.AddCommand(litsCmd)
}

func runLits(_ *cobra.Command, args []string) error {
	start := time.Now()
	value := args[0]

	compact, lim, off, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, "lits")
	if err != nil {
		return err
	}
	defer s.Close()

	refs, err := query.FindLiteralRefs(s.DB(), value)
	if err != nil {
		return fmt.Errorf("find literal refs: %w", err)
	}

	if len(refs) == 0 {
		return w.WriteError("lits", &output.Error{
			Code:    output.ErrNotFound,
			Message: fmt.Sprintf("no string refs found for %q", value),
		})
	}

	results := make([]output.Result, 0, len(refs))
	for i := range refs {
		r := &refs[i]
		displayPath := r.FilePathRel
		if displayPath == "" {
			displayPath = r.FilePath
		}
		refRange := output.Range{
			Start: output.Position{Line: r.Line, Col: r.Col},
			End:   output.Position{Line: r.Line, Col: r.Col},
		}
		res := output.Result{
			ID:      r.ID,
			File:    displayPath,
			FileAbs: r.FilePath,
			Range:   refRange,
			Kind:    r.Kind,
			Name:    value,
			Match:   r.Snippet,
		}
		if r.Name != "" {
			res.Name = r.Name
		}
		if r.EnclosingID != "" {
			res.Enclosing = &output.Enclosing{ID: r.EnclosingID}
		}
		results = append(results, res)
	}

	total := len(results)
	if off < len(results) {
		results = results[off:]
	} else {
		results = nil
	}
	if lim > 0 && len(results) > lim {
		results = results[:lim]
	}

	resp := output.Response[output.Result]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:    "lits",
			Query:      map[string]string{"value": value},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      total,
			Offset:     off,
			Limit:      lim,
			Truncated:  len(results) < total,
		},
	}

	return w.WriteResponse(resp)
}
