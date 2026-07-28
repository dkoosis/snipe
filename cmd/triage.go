package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// triageFileRow is one file's bundled fact set. Score/FanIn/Cyclo are
// pointers so a file with no hotspots row (no git history / not indexed)
// serializes as null rather than being silently dropped — the value-add over
// raw `snipe hotspots`, which omits such files entirely (AC line 2).
type triageFileRow struct {
	Path    string   `json:"path"`
	Score   *float64 `json:"score"`
	FanIn   *int     `json:"fan_in"`
	Cyclo   *int     `json:"cyclo"`
	Package string   `json:"package"`
}

// triageResponse is the bespoke JSON contract for `snipe triage`. It skips
// the output.Response envelope (protocol/ok/meta) on purpose: the consumer is
// a bash gate (cc-plugins significance.sh) that wants exactly these four
// fields — facts only, no band/route decision (D4 token budget; that policy
// lives in the consumer, not snipe). Slices are always non-nil so empty
// groups serialize as [] rather than null (AXI #5).
type triageResponse struct {
	Files             []triageFileRow `json:"files"`
	PackageCount      int             `json:"package_count"`
	FilesWithTests    []string        `json:"files_with_tests"`
	FilesWithoutTests []string        `json:"files_without_tests"`
}

// runTriage bundles the three reads a triage gate needs over a file set —
// hotspots row, real Go package, test-proximity — into one call, so a caller
// replaces "1 hotspots + N tests + dirname math" with one command.
func runTriage(files []string) error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, dir, err := OpenStore(w, cmdNameTriage)
	if err != nil {
		return err
	}
	defer s.Close()

	resp := buildTriageResponse(s, dir, files)

	if GetOutputFormat() == output.OutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	return writeTriageText(resp)
}

// buildTriageResponse assembles the triage facts for a file set. Every input
// file lands in Files (null score/fan_in/cyclo when unscored) and in exactly
// one of FilesWithTests/FilesWithoutTests. Hotspot-read failures degrade to
// "no scores" rather than failing the whole command — package and
// test-proximity facts are independent of the hotspots table.
func buildTriageResponse(s *store.Store, dir string, files []string) triageResponse {
	hotByPath := map[string]hotspotRow{}
	if rows, _, err := loadHotspotRows(s); err == nil {
		for _, r := range rows {
			hotByPath[r.Path] = r
		}
	}

	fileRows := make([]triageFileRow, 0, len(files))
	pkgSeen := make(map[string]struct{}, len(files))
	withTests := make([]string, 0, len(files))
	withoutTests := make([]string, 0, len(files))

	for _, f := range files {
		rel := triageRelPath(dir, f)

		row := triageFileRow{Path: rel}
		if hr, ok := hotByPath[rel]; ok {
			score := hr.Score
			fanIn := hr.FanIn
			cyclo := hr.Cyclo
			row.Score = &score
			row.FanIn = &fanIn
			row.Cyclo = &cyclo
		}
		row.Package = resolveFilePackage(s.DB(), rel)
		if row.Package != "" {
			pkgSeen[row.Package] = struct{}{}
		}
		fileRows = append(fileRows, row)

		if hasFileTests(s.DB(), rel) {
			withTests = append(withTests, rel)
		} else {
			withoutTests = append(withoutTests, rel)
		}
	}

	return triageResponse{
		Files:             fileRows,
		PackageCount:      len(pkgSeen),
		FilesWithTests:    withTests,
		FilesWithoutTests: withoutTests,
	}
}

// triageRelPath normalizes a CLI-supplied file argument (absolute, or
// relative to CWD) into the repo-root-relative, forward-slashed form used as
// the join key across symbols.file_path_rel, graph_metrics, and file_churn.
func triageRelPath(root, f string) string {
	if !filepath.IsAbs(f) {
		if abs, err := filepath.Abs(f); err == nil {
			f = abs
		}
	}
	return relToRoot(root, f)
}

// resolveFilePackage looks up the real Go package for a file via the
// symbols table's pkg_path column — NOT path-dirname (plan: package must be
// the real Go package). Returns "" when the file has no indexed symbols.
func resolveFilePackage(db *sql.DB, rel string) string {
	var pkg sql.NullString
	err := db.QueryRow(`
		SELECT pkg_path FROM symbols
		WHERE file_path_rel = ? AND pkg_path IS NOT NULL AND pkg_path <> ''
		ORDER BY pkg_path
		LIMIT 1
	`, rel).Scan(&pkg)
	if err != nil {
		return ""
	}
	return pkg.String
}

// hasFileTests reports whether any test exercises any symbol declared in the
// file — the file-level test-presence signal triage actually wants. It replaces
// the old `snipe tests --at <file>:1:1` idiom (and significance.sh's
// has_nearby_tests), which anchored on a symbol starting at line 1; real symbols
// almost never do, so that check reported "no tests" for well-tested files.
// Enumerate the file's symbols, then FindTestsMulti over their IDs: one query,
// correct at the file granularity.
func hasFileTests(db *sql.DB, rel string) bool {
	// FindSymbolsInFile takes a literal SQL LIMIT (0 = zero rows, not unlimited);
	// 1000 comfortably covers every symbol in a single Go file.
	syms, err := query.FindSymbolsInFile(db, rel, 1000, 0)
	if err != nil || len(syms) == 0 {
		return false
	}
	ids := make([]string, 0, len(syms))
	for _, s := range syms {
		ids = append(ids, s.ID)
	}
	rows, err := query.FindTestsMulti(db, ids, false, 1, 0)
	if err != nil {
		return false
	}
	return len(rows) > 0
}

func writeTriageText(resp triageResponse) error {
	var b strings.Builder
	fmt.Fprintf(&b, "triage · %d files, %d packages, %d with tests, %d without\n",
		len(resp.Files), resp.PackageCount, len(resp.FilesWithTests), len(resp.FilesWithoutTests))
	if len(resp.Files) == 0 {
		b.WriteString("  (no files)\n")
		_, err := os.Stdout.WriteString(b.String())
		return err
	}
	fmt.Fprintf(&b, "  %6s %6s %6s  %-24s %s\n", "score", "fan_in", "cyclo", "package", "path")
	for _, f := range resp.Files {
		score, fanIn, cyclo := "-", "-", "-"
		if f.Score != nil {
			score = fmt.Sprintf("%.2f", *f.Score)
		}
		if f.FanIn != nil {
			fanIn = fmt.Sprintf("%d", *f.FanIn)
		}
		if f.Cyclo != nil {
			cyclo = fmt.Sprintf("%d", *f.Cyclo)
		}
		fmt.Fprintf(&b, "  %6s %6s %6s  %-24s %s\n", score, fanIn, cyclo, f.Package, f.Path)
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
