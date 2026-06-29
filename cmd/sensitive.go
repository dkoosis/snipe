package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/sensitive"
)

const cmdNameSensitive = "sensitive"

type sensitiveRow struct {
	Path  string           `json:"path"`
	Zones []sensitive.Zone `json:"zones"`
}

// runSensitive lists indexed Go files that fall in a security-sensitive zone
// (auth, crypto, migration, secret, payment) — code that is always
// significant regardless of size or churn (Tornhill). Zones come from
// built-in heuristics plus optional .snipe/sensitive globs.
func runSensitive() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, dir, err := OpenStore(w, cmdNameSensitive)
	if err != nil {
		return err
	}
	defer s.Close()
	start := time.Now()

	files, err := s.GetAllFiles()
	if err != nil {
		return w.WriteError(cmdNameSensitive, &output.Error{Code: output.ErrInternal, Message: err.Error()})
	}
	cls := sensitive.Load(dir)

	var rows []sensitiveRow
	for path := range files {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		rel := path
		if r, err := filepath.Rel(dir, path); err == nil {
			rel = r
		}
		if z := cls.Zones(rel); len(z) > 0 {
			rows = append(rows, sensitiveRow{Path: rel, Zones: z})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[sensitiveRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  cmdNameSensitive,
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    len(rows),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "sensitive zones · %d files (always-significant code: auth/crypto/migration/secret/payment)\n", len(rows))
	if len(rows) == 0 {
		b.WriteString("  (none detected — built-in heuristics + optional .snipe/sensitive globs)\n")
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-28s  %s\n", zonesString(r.Zones), r.Path)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

func zonesString(zones []sensitive.Zone) string {
	parts := make([]string, len(zones))
	for i, z := range zones {
		parts[i] = string(z)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
