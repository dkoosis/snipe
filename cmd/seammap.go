package cmd

// sn-l1kh.5: seam map — surface where THIS repo is intentionally extensible.
//
// A "seam" here is an interface that (a) has at least one concrete
// implementation and (b) is leaned on from more than one call site — an
// interface satisfied by exactly one type and referenced nowhere outside its
// own file is dependency inversion in name only, not a real extension point.
//
// Reuses query.FindImplementers (the same lookup `snipe impl` uses) and
// query.FindRefs (the same lookup `snipe refs`/lifecycle use) rather than
// inventing new traversal. The new surface here is the ranking (fan-in +
// implementation count -> one score) and the docs/diagrams/seam-map.md
// render, which reuses diagram.Builder + emitDoc/writeDiagramDoc from
// cmd/diagram.go (sn-l1kh.1) unchanged.

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/diagram"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// minSeamFanIn excludes interfaces touched by fewer than this many distinct
// call-site files (outside the interface's own definition file). "A seam
// touched by 1 caller isn't really a seam" (sn-l1kh.5 acceptance) — it's
// dependency inversion nobody depends on yet.
const minSeamFanIn = 2

// seamInterface is the minimal projection of an interface symbol needed to
// build a Seam. Populated by a direct query rather than query.LookupByID,
// since we only need a handful of columns across potentially many rows.
type seamInterface struct {
	ID          string
	Name        string
	PkgPath     string
	FilePath    string
	FilePathRel string
	LineStart   int
}

// Seam is one ranked extension point: an interface, its implementations, and
// how much other code leans on it.
type Seam struct {
	Interface seamInterface
	Impls     []query.SymbolRow
	FanInDeg  int // distinct call-site files referencing the interface, excluding its own def file
	FanInPkgs int // distinct packages among those call sites
	Score     int
}

// scoreSeam combines fan-in and implementation count into one ranking score.
// Fan-in is weighted higher than implementation count: a seam's value comes
// from how widely other code depends on the abstraction, not merely from how
// many concrete types happen to satisfy it (an interface can be a genuine
// seam with just one implementation, e.g. for testability).
func scoreSeam(fanInPkgs, implCount int) int {
	return fanInPkgs*2 + implCount
}

// rankSeams filters out interfaces that aren't real seams (no implementation,
// or fan-in below minSeamFanIn), scores the rest, and sorts by score
// descending, breaking ties by name for determinism.
func rankSeams(seams []Seam) []Seam {
	out := make([]Seam, 0, len(seams))
	for i := range seams {
		if len(seams[i].Impls) == 0 || seams[i].FanInDeg < minSeamFanIn {
			continue
		}
		seams[i].Score = scoreSeam(seams[i].FanInPkgs, len(seams[i].Impls))
		out = append(out, seams[i])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Interface.Name < out[j].Interface.Name
	})
	return out
}

// seamFanIn computes distinct call-site files and distinct packages that
// reference an interface type, given its usage refs (query.FindRefs output)
// and a file->package lookup. Refs inside defFile (the interface's own
// definition file) are excluded — a self-reference (e.g. an embedding alias
// declared alongside the interface) doesn't demonstrate the interface being
// leaned on elsewhere.
func seamFanIn(refs []query.RefRow, defFile string, fileToPkg map[string]string) (sites, pkgs int) {
	siteSet := make(map[string]bool, len(refs))
	pkgSet := make(map[string]bool, len(refs))
	for i := range refs {
		if refs[i].FilePath == defFile {
			continue
		}
		siteSet[refs[i].FilePath] = true
		if pkg := fileToPkg[refs[i].FilePath]; pkg != "" {
			pkgSet[pkg] = true
		}
	}
	return len(siteSet), len(pkgSet)
}

// findInterfaceSymbols enumerates interfaces defined in-repo (excludes
// symbols outside the indexed tree and _test.go-only interfaces, which aren't
// real extension points).
func findInterfaceSymbols(db *sql.DB) ([]seamInterface, error) {
	rows, err := db.Query(`
		SELECT id, name, pkg_path, file_path, file_path_rel, line_start
		FROM symbols
		WHERE kind = ?
		  AND file_path_rel != ''
		  AND file_path NOT LIKE '%_test.go'
		ORDER BY name, file_path_rel
	`, cmdKindInterface)
	if err != nil {
		return nil, fmt.Errorf("query interfaces: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []seamInterface
	for rows.Next() {
		var si seamInterface
		if err := rows.Scan(&si.ID, &si.Name, &si.PkgPath, &si.FilePath, &si.FilePathRel, &si.LineStart); err != nil {
			return nil, fmt.Errorf("scan interface row: %w", err)
		}
		out = append(out, si)
	}
	return out, rows.Err()
}

// fileToPkgMap builds an absolute-file-path -> package-path lookup from the
// symbols table (every file belongs to exactly one package, so any symbol
// defined in a file names that file's package).
func fileToPkgMap(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT DISTINCT file_path, pkg_path FROM symbols WHERE pkg_path != '' AND file_path != ''`)
	if err != nil {
		return nil, fmt.Errorf("query file->pkg map: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var file, pkg string
		if err := rows.Scan(&file, &pkg); err != nil {
			return nil, fmt.Errorf("scan file->pkg row: %w", err)
		}
		out[file] = pkg
	}
	return out, rows.Err()
}

// buildSeams enumerates every in-repo interface, finds its implementations
// and fan-in, and returns the unranked, unfiltered set. rankSeams applies the
// seam threshold and ordering.
func buildSeams(db *sql.DB) ([]Seam, error) {
	ifaces, err := findInterfaceSymbols(db)
	if err != nil {
		return nil, err
	}
	fileToPkg, err := fileToPkgMap(db)
	if err != nil {
		return nil, err
	}

	seams := make([]Seam, 0, len(ifaces))
	for _, iface := range ifaces {
		// Cap well above realistic fan-out; this is a comprehension view, not
		// a paginated listing.
		impls, err := query.FindImplementers(db, iface.ID, 1000, 0)
		if err != nil {
			return nil, fmt.Errorf("find implementers of %s: %w", iface.Name, err)
		}
		refs, err := query.FindRefs(db, iface.ID, 5000, 0)
		if err != nil {
			return nil, fmt.Errorf("find refs of %s: %w", iface.Name, err)
		}
		sites, pkgs := seamFanIn(refs, iface.FilePath, fileToPkg)
		seams = append(seams, Seam{
			Interface: iface,
			Impls:     impls,
			FanInDeg:  sites,
			FanInPkgs: pkgs,
		})
	}
	return seams, nil
}

// seamNodeStyle highlights the top-3 ranked seams so the diagram's visual
// hierarchy matches the ranking, mirroring nodeStyleForRank's tiering for the
// arch diagram (pos is 1-indexed rank within the rendered set).
func seamNodeStyle(pos int) map[string]string {
	switch {
	case pos <= 3:
		return map[string]string{diagramFill: diagramColorWarn, diagramBold: diagramTrue}
	case pos <= 10:
		return map[string]string{diagramFill: "#fef3c7"}
	default:
		return nil
	}
}

// perSeamImplCap bounds how many implementations are drawn per seam node so
// a widely-implemented interface (e.g. an error type or io.Writer-shaped
// thing) doesn't blow up the diagram.
const perSeamImplCap = 6

// buildSeamMap renders the Claude-summary text and D2 source for a ranked
// seam list, following the same emit shape as runDiagramArch/Flow/Lifecycle.
func buildSeamMap(seams []Seam, totalInterfaces int) (summary, d2src string) {
	var b diagram.Builder
	b.Title = "snipe seam map · pluggable interfaces / extension points"
	b.Direction = diagramDirRight

	var sb strings.Builder
	fmt.Fprintf(&sb, "seam map · %d seams ranked (of %d interfaces scanned) · score = fan_in_pkgs*2 + impl_count\n", len(seams), totalInterfaces)
	if len(seams) == 0 {
		sb.WriteString("  no seams found — either no interfaces have implementations, or none clear the fan-in>=2 bar\n")
	}
	for i := range seams {
		s := &seams[i]
		pos := i + 1
		fmt.Fprintf(&sb, "%2d. %-28s impl=%-3d fan_pkgs=%-3d fan_sites=%-3d score=%-3d  %s:%d\n",
			pos, s.Interface.Name, len(s.Impls), s.FanInPkgs, s.FanInDeg, s.Score,
			s.Interface.FilePathRel, s.Interface.LineStart)

		nodeID := diagram.SanitizeID(fmt.Sprintf("seam_%s", s.Interface.ID))
		label := fmt.Sprintf("%s  (impl=%d fan=%d)", s.Interface.Name, len(s.Impls), s.FanInPkgs)
		b.AddNode(nodeID, label, "diamond", seamNodeStyle(pos))

		shown := s.Impls
		truncated := 0
		if len(shown) > perSeamImplCap {
			truncated = len(shown) - perSeamImplCap
			shown = shown[:perSeamImplCap]
		}
		for j := range shown {
			impl := &shown[j]
			implLabel := impl.Name
			if impl.FilePathRel != "" {
				fmt.Fprintf(&sb, "      - %s  (%s)\n", impl.Name, impl.FilePathRel)
			} else {
				fmt.Fprintf(&sb, "      - %s\n", impl.Name)
			}
			implID := nodeID + "_" + diagram.SanitizeID(impl.PkgPath+"."+impl.Name)
			b.AddNode(implID, implLabel, "", nil)
			b.AddDashedEdge(implID, nodeID, "implements")
		}
		if truncated > 0 {
			fmt.Fprintf(&sb, "      + %d more implementations\n", truncated)
			moreID := nodeID + "_more"
			b.AddNode(moreID, fmt.Sprintf("+%d more", truncated), "", map[string]string{"font-color": "#6b7280"})
			b.AddDashedEdge(moreID, nodeID, "implements")
		}
	}

	return sb.String(), b.Render()
}

// diagramSeamsTopN caps how many ranked seams are rendered; set by
// DiagramSeamsCmd.Run (cmd/commands.go).
var diagramSeamsTopN int

// runDiagramSeams is the Kong entry point for `snipe diagram seams`.
func runDiagramSeams() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram seams")
	if err != nil {
		return err
	}
	defer s.Close()

	all, err := buildSeams(s.DB())
	if err != nil {
		return fmt.Errorf("build seams: %w", err)
	}
	ranked := rankSeams(all)
	if diagramSeamsTopN > 0 && len(ranked) > diagramSeamsTopN {
		ranked = ranked[:diagramSeamsTopN]
	}

	summary, d2src := buildSeamMap(ranked, len(all))
	return emitDoc("seam-map", summary, d2src)
}
