package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
)

var (
	deadIncludeTests bool
	deadPkg          string
)

// deadcodeCmd reports exported symbols with zero non-test references.
// One batch query replaces the per-symbol 'snipe refs' loop lintbrush's
// api-surface linter currently runs (snipe-sbi).

// deadcodeRow is the JSON shape per dead export.
type deadcodeRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Pkg     string `json:"pkg"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	RefsAll int    `json:"refs_all"` // count including tests
}

func runDeadcode() error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	s, dir, err := OpenStore(w, "deadcode")
	if err != nil {
		return err
	}
	defer s.Close()

	// Refs count: include vs. exclude tests selected by the file_path filter.
	refsCountExpr := `(SELECT COUNT(*) FROM refs r WHERE r.symbol_id = s.id`
	if !deadIncludeTests {
		refsCountExpr += ` AND r.file_path NOT LIKE '%_test.go'`
	}
	refsCountExpr += `)`

	// Also pull total (always all-files) so callers can see the gap.
	totalCountExpr := `(SELECT COUNT(*) FROM refs r WHERE r.symbol_id = s.id)`

	q := `
		SELECT s.id, s.name, s.kind, s.pkg_path, s.file_path_rel, s.line_start, ` + totalCountExpr + ` AS refs_all
		FROM symbols s
		WHERE s.name GLOB '[A-Z]*'
		  AND s.kind IN ('func','method','type','const','var')
		  AND s.file_path NOT LIKE '%_test.go'
		  AND ` + refsCountExpr + ` = 0
		  AND NOT (s.name = 'main' AND s.kind = 'func')`
	args := []interface{}{}
	if deadPkg != "" {
		q += ` AND (s.pkg_path = ? OR s.pkg_path LIKE ? OR s.pkg_path LIKE ?)`
		args = append(args, deadPkg, "%/"+deadPkg, "%"+deadPkg+"%")
	}
	q += ` ORDER BY s.pkg_path, s.name`

	rows, err := s.DB().Query(q, args...)
	if err != nil {
		return w.WriteError("deadcode", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}
	defer func() { _ = rows.Close() }()

	out := []deadcodeRow{}
	for rows.Next() {
		var r deadcodeRow
		var fileRel sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Pkg, &fileRel, &r.Line, &r.RefsAll); err != nil {
			continue
		}
		r.File = fileRel.String
		out = append(out, r)
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[deadcodeRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  out,
			Meta: output.Meta{
				Command:  "deadcode",
				Query:    map[string]string{"include_tests": fmt.Sprintf("%t", deadIncludeTests), flagPkg: deadPkg},
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    len(out),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "deadcode · %d unreferenced exports", len(out))
	if !deadIncludeTests {
		b.WriteString(" (test refs ignored)")
	}
	b.WriteString("\n")
	for _, r := range out {
		fmt.Fprintf(&b, "  %s\t%s\t%s:%d\t%s\n", r.Kind, r.Pkg+"."+r.Name, r.File, r.Line, refsHint(r.RefsAll, deadIncludeTests))
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

func refsHint(refsAll int, includeTests bool) string {
	if includeTests {
		return ""
	}
	if refsAll == 0 {
		return ""
	}
	return fmt.Sprintf("(refs_all=%d, all from tests)", refsAll)
}
