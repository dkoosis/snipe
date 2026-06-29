package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/store"
)

const kindChurn = "churn"

// runChurnMetrics renders the git change-frequency table populated by
// `snipe index` (see internal/gitchurn). Files rank by commit count — the
// CodeScene "revisions" measure, and the temporal axis of the hotspot model
// (Tornhill, Your Code as a Crime Scene; Nagappan & Ball, ICSE 2005). An
// empty table means a non-git checkout or an un-indexed repo, not an error.
func runChurnMetrics(s *store.Store, dir string, startedAt time.Time) error {
	rows, err := s.ReadFileChurnTopN(0) // read all; filter/truncate below
	if err != nil {
		return fmt.Errorf("read file churn: %w", err)
	}
	if metricsPkg != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if strings.Contains(r.Path, metricsPkg) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	if metricsTopN > 0 && len(rows) > metricsTopN {
		rows = rows[:metricsTopN]
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[store.FileChurn]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  cmdNameMetrics,
				Query:    map[string]string{jsonKeyKind: kindChurn, flagPkg: metricsPkg},
				RepoRoot: dir,
				Ms:       time.Since(startedAt).Milliseconds(),
				Total:    len(rows),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	if len(rows) == 0 {
		b.WriteString("git history · churn · (no rows — not a git repo, or run `snipe index` to populate)\n")
		_, err = os.Stdout.WriteString(b.String())
		return err
	}
	fmt.Fprintf(&b, "git history · churn · %d files (commits = revisions; authors = distinct committers / bus factor)\n", len(rows))
	fmt.Fprintf(&b, "  %7s %7s  %-12s  %s\n", "commits", "authors", "last-changed", "path")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %7d %7d  %-12s  %s\n", r.Commits, r.Authors, r.LastChanged, r.Path)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}
