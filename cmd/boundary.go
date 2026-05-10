package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	boundaryDetailed bool
	boundaryDir      string
)

var boundaryCmd = &cobra.Command{
	Use:     "boundary <pkg-set-a> <pkg-set-b>",
	Short:   "Show symbols whose refs cross between two package sets",
	GroupID: categoryAdvanced,
	Long: `Find every symbol whose references cross the boundary between two
package sets — the data needed to plan a module split.

Patterns:
  internal/store          exact: just that package
  internal/store/...      recursive: that package and descendants

Examples:
  snipe boundary internal/store internal/query
  snipe boundary 'internal/store/...' 'internal/query/...'
  snipe boundary --detailed internal/store internal/query
  snipe boundary --direction=a-to-b internal/store internal/query`,
	Args: cobra.ExactArgs(2),
	RunE: runBoundary,
}

func init() {
	boundaryCmd.Flags().BoolVar(&boundaryDetailed, "detailed", false,
		"Include per-ref file:line for every crossing")
	boundaryCmd.Flags().StringVar(&boundaryDir, "direction", "both",
		"both | a-to-b | b-to-a")
	rootCmd.AddCommand(boundaryCmd)
}

func runBoundary(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, "boundary")
	if err != nil {
		return err
	}
	defer s.Close()

	patternsA := []string{args[0]}
	patternsB := []string{args[1]}

	universe, err := allPkgPaths(s.DB())
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}

	setA := query.MatchPackagePatterns(universe, patternsA)
	setB := query.MatchPackagePatterns(universe, patternsB)

	if len(setA) == 0 || len(setB) == 0 {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrNotFound,
			Message: fmt.Sprintf("no packages matched: A=%d B=%d (patterns: %q %q)",
				len(setA), len(setB), args[0], args[1]),
		})
	}

	report, err := query.FindBoundaryCrossings(s.DB(), setA, setB)
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}

	if boundaryDetailed {
		if err := query.PopulateBoundaryLocations(s.DB(), report); err != nil {
			return w.WriteError("boundary", &output.Error{
				Code: output.ErrInternal, Message: err.Error(),
			})
		}
	}

	result := buildBoundaryResult(report)
	resp := output.Response[output.BoundaryResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.BoundaryResult{result},
		Meta: output.Meta{
			Command:    "boundary",
			Query:      map[string]string{"a": args[0], "b": args[1], "direction": boundaryDir},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      countCrossings(report),
		},
	}
	return w.WriteResponse(resp)
}

func allPkgPaths(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT pkg_path FROM symbols WHERE pkg_path != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func buildBoundaryResult(r *query.BoundaryReport) output.BoundaryResult {
	res := output.BoundaryResult{SetA: r.SetA, SetB: r.SetB}
	if boundaryDir == "both" || boundaryDir == "a-to-b" {
		res.Directions = append(res.Directions, makeDir("A", "B", r.AToB))
	}
	if boundaryDir == "both" || boundaryDir == "b-to-a" {
		res.Directions = append(res.Directions, makeDir("B", "A", r.BToA))
	}
	return res
}

func makeDir(from, to string, refs []query.BoundaryRef) output.BoundaryDirection {
	d := output.BoundaryDirection{From: from, To: to, Symbols: make([]output.BoundarySymbol, len(refs))}
	for i, b := range refs {
		d.Symbols[i] = output.BoundarySymbol{
			Symbol:    b.Symbol,
			Kind:      b.Kind,
			SourcePkg: b.SourcePkg,
			TargetPkg: b.TargetPkg,
			RefCount:  b.RefCount,
			Locations: convertLocs(b.Locations),
		}
		d.Total += b.RefCount
	}
	return d
}

func convertLocs(in []query.BoundaryLoc) []output.BoundaryLoc {
	if len(in) == 0 {
		return nil
	}
	out := make([]output.BoundaryLoc, len(in))
	for i, l := range in {
		out[i] = output.BoundaryLoc{File: l.File, Line: l.Line}
	}
	return out
}

func countCrossings(r *query.BoundaryReport) int {
	n := 0
	for _, b := range r.AToB {
		n += b.RefCount
	}
	for _, b := range r.BToA {
		n += b.RefCount
	}
	return n
}
