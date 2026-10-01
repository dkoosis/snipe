package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	deadIncludeTests bool
	deadPkg          string
)

// deadcodeCmd reports exported symbols with zero non-test references.
// One batch query replaces the per-symbol 'snipe refs' loop lintbrush's
// api-surface linter currently runs (snipe-sbi).

func runDeadcode() error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	s, dir, err := OpenStore(w, "deadcode")
	if err != nil {
		return err
	}
	defer s.Close()

	out, err := query.FindDeadExports(s.DB(), deadPkg, deadIncludeTests)
	if err != nil {
		return w.WriteError("deadcode", &protocol.Error{
			Code: protocol.ErrInternal, Message: err.Error(),
		})
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := protocol.Response[query.DeadExport]{
			Protocol: protocol.ProtocolVersion,
			Ok:       true,
			Results:  out,
			Meta: protocol.Meta{
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
