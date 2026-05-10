package cmd

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var traceCmd = &cobra.Command{
	Use:     "trace <value>",
	Short:   "Trace a string literal through its call context",
	GroupID: categoryCore,
	Long: `Finds all occurrences of a string literal and shows the enclosing function
and immediate callers — useful for env-var and config-key propagation.

Falls back to substring match when exact match returns no results.

Examples:
  snipe trace SNIPE_VOYAGE_API_KEY   # Trace env var through call chain
  snipe trace "my-config-key"        # Trace a config key`,
	Args: cobra.ExactArgs(1),
	RunE: runTrace,
}

func init() {
	rootCmd.AddCommand(traceCmd)
}

func runTrace(_ *cobra.Command, args []string) error {
	start := time.Now()
	value := args[0]

	_, lim, off, _, _, _ := GetOutputConfig()
	compact := false
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, "trace")
	if err != nil {
		return err
	}
	defer s.Close()

	refs, err := query.TraceString(s.DB(), value)
	if err != nil {
		return w.WriteError("trace", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Fallback: substring search when exact match finds nothing.
	if len(refs) == 0 {
		litRefs, ferr := query.FindLiteralRefsSubstring(s.DB(), value)
		if ferr == nil && len(litRefs) > 0 {
			refs = query.EnrichTraceRefs(s.DB(), litRefs)
		}
	}

	if len(refs) == 0 {
		return w.WriteError("trace", &output.Error{
			Code:    output.ErrNotFound,
			Message: "no string refs found for " + value,
		})
	}

	total := len(refs)
	if off < len(refs) {
		refs = refs[off:]
	} else {
		refs = nil
	}
	if lim > 0 && len(refs) > lim {
		refs = refs[:lim]
	}

	results := make([]output.TraceResult, 0, len(refs))
	for _, r := range refs {
		displayPath := r.FilePathRel
		if displayPath == "" {
			displayPath = r.FilePath
		}
		tr := output.TraceResult{
			ID:        r.ID,
			Value:     r.Value,
			File:      displayPath,
			Line:      r.Line,
			Col:       r.Col,
			Kind:      r.Kind,
			Snippet:   r.Snippet,
			Enclosing: r.Enclosing,
			Callers:   r.Callers,
		}
		results = append(results, tr)
	}

	resp := output.Response[output.TraceResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:    "trace",
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
