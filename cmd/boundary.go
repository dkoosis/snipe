package cmd

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var (
	boundaryDetailed bool
	boundaryDir      string
	boundaryLayers   string
)

func runBoundary(args []string) error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	s, dir, err := OpenStore(w, "boundary")
	if err != nil {
		return err
	}
	defer s.Close()

	if boundaryLayers != "" {
		return runBoundaryLayers(s, dir, start)
	}

	if len(args) != 2 {
		return w.WriteError("boundary", &output.Error{
			Code:    output.ErrInternal,
			Message: "boundary requires <pkg-set-a> <pkg-set-b> unless --layers is set",
		})
	}

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

// archManifest is a minimal subset of go-arch-lint's manifest schema — just
// enough to extract layer definitions for snipe-0bh. We don't enforce or
// interpret deps rules; we only need {layer-name -> []path-pattern}.
type archManifest struct {
	Components map[string]struct {
		In interface{} `yaml:"in"` // string or []string
	} `yaml:"components"`
}

// layerCrossingRow is one A→B (or B→A) layer pair with its boundary stats.
type layerCrossingRow struct {
	From         string `json:"from"`
	To           string `json:"to"`
	Crossings    int    `json:"crossings"`
	Symbols      int    `json:"symbols"`
	FromPackages int    `json:"from_packages"`
	ToPackages   int    `json:"to_packages"`
}

func runBoundaryLayers(s *store.Store, dir string, start time.Time) error {
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	data, err := os.ReadFile(boundaryLayers)
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: "read layers manifest: " + err.Error(),
		})
	}
	var man archManifest
	if err := yaml.Unmarshal(data, &man); err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: "parse layers manifest: " + err.Error(),
		})
	}

	// Normalize go-arch-lint glob patterns to snipe's pattern dialect:
	// '<path>/**' (recursive)         -> '<path>/...'
	// '<path>/**/<rest>'              -> '<path>/...' (broaden; snipe lacks mid-glob)
	// '<path>/*' (one level)          -> '<path>/...' (treat as recursive — snipe has no one-level form)
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, p := range in {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			switch {
			case strings.HasSuffix(p, "/**"):
				p = strings.TrimSuffix(p, "/**") + "/..."
			case strings.HasSuffix(p, "/*"):
				p = strings.TrimSuffix(p, "/*") + "/..."
			default:
				if idx := strings.Index(p, "/**/"); idx > 0 {
					p = p[:idx] + "/..."
				}
			}
			out = append(out, p)
		}
		return out
	}

	type layer struct {
		name     string
		patterns []string
		pkgs     []string
	}
	var layers []layer
	for name, comp := range man.Components {
		var patterns []string
		switch v := comp.In.(type) {
		case string:
			patterns = []string{v}
		case []interface{}:
			for _, x := range v {
				if str, ok := x.(string); ok {
					patterns = append(patterns, str)
				}
			}
		}
		layers = append(layers, layer{name: name, patterns: norm(patterns)})
	}
	sort.Slice(layers, func(i, j int) bool { return layers[i].name < layers[j].name })

	universe, err := allPkgPaths(s.DB())
	if err != nil {
		return w.WriteError("boundary", &output.Error{
			Code: output.ErrInternal, Message: err.Error(),
		})
	}
	for i := range layers {
		layers[i].pkgs = query.MatchPackagePatterns(universe, layers[i].patterns)
	}

	// One FindBoundaryCrossings per ordered pair (i->j and j->i are both
	// reported via the directional A→B/B→A split inside FindBoundaryCrossings).
	rows := make([]layerCrossingRow, 0, len(layers)*(len(layers)-1))
	for i := 0; i < len(layers); i++ {
		for j := i + 1; j < len(layers); j++ {
			if len(layers[i].pkgs) == 0 || len(layers[j].pkgs) == 0 {
				continue
			}
			report, err := query.FindBoundaryCrossings(s.DB(), layers[i].pkgs, layers[j].pkgs)
			if err != nil {
				return w.WriteError("boundary", &output.Error{
					Code: output.ErrInternal, Message: err.Error(),
				})
			}
			rows = append(rows,
				layerCrossingRow{
					From: layers[i].name, To: layers[j].name,
					Crossings: sumRefs(report.AToB), Symbols: len(report.AToB),
					FromPackages: len(layers[i].pkgs), ToPackages: len(layers[j].pkgs),
				},
				layerCrossingRow{
					From: layers[j].name, To: layers[i].name,
					Crossings: sumRefs(report.BToA), Symbols: len(report.BToA),
					FromPackages: len(layers[j].pkgs), ToPackages: len(layers[i].pkgs),
				},
			)
		}
	}

	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[layerCrossingRow]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results:  rows,
			Meta: output.Meta{
				Command:  "boundary",
				Query:    map[string]string{"layers": boundaryLayers},
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
	fmt.Fprintf(&b, "layers · %d ordered pairs (%d layers)\n", len(rows), len(layers))
	for _, r := range rows {
		if r.Crossings == 0 && r.Symbols == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s -> %s\tcrossings=%d  symbols=%d  (%d->%d pkgs)\n",
			r.From, r.To, r.Crossings, r.Symbols, r.FromPackages, r.ToPackages)
	}
	_, err = os.Stdout.WriteString(b.String())
	return err
}

func sumRefs(refs []query.BoundaryRef) int {
	n := 0
	for _, r := range refs {
		n += r.RefCount
	}
	return n
}
