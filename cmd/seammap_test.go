package cmd

import (
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

// sn-l1kh.5: scoreSeam weights fan-in (packages leaning on the abstraction)
// over raw implementation count — a widely-depended-on interface with one
// implementation still matters more than a rarely-used interface with many.
func TestScoreSeam(t *testing.T) {
	cases := []struct {
		name             string
		fanInPkgs, impls int
		want             int
	}{
		{"zero everything", 0, 0, 0},
		{"fan-in dominates", 5, 1, 11},
		{"impl-heavy but low fan-in", 1, 8, 10},
		{"balanced", 3, 3, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scoreSeam(c.fanInPkgs, c.impls)
			if got != c.want {
				t.Errorf("scoreSeam(%d, %d) = %d, want %d", c.fanInPkgs, c.impls, got, c.want)
			}
		})
	}
}

// seamFanIn must exclude refs inside the interface's own definition file
// (self-references don't demonstrate the interface being leaned on
// elsewhere) and must dedupe by file and by package.
func TestSeamFanIn(t *testing.T) {
	fileToPkg := map[string]string{
		"/repo/internal/store/store.go":  "internal/store",
		"/repo/internal/store/sqlite.go": "internal/store",
		"/repo/cmd/impl.go":              "cmd",
		"/repo/internal/query/lookup.go": "internal/query",
	}
	defFile := "/repo/internal/store/store.go"

	refs := []query.RefRow{
		{FilePath: defFile},                          // same-file: excluded
		{FilePath: "/repo/internal/store/sqlite.go"}, // same pkg as def, different file
		{FilePath: "/repo/internal/store/sqlite.go"}, // duplicate file: deduped
		{FilePath: "/repo/cmd/impl.go"},              // different pkg
		{FilePath: "/repo/internal/query/lookup.go"}, // different pkg
		{FilePath: "/repo/unmapped/file.go"},         // no pkg mapping: counts as a site, not a pkg
	}

	sites, pkgs := seamFanIn(refs, defFile, fileToPkg)
	if sites != 4 {
		t.Errorf("sites = %d, want 4 (distinct non-def files: sqlite.go, impl.go, lookup.go, unmapped/file.go)", sites)
	}
	if pkgs != 3 {
		t.Errorf("pkgs = %d, want 3 (internal/store, cmd, internal/query)", pkgs)
	}
}

func TestSeamFanIn_AllExcludedWhenOnlySelfFile(t *testing.T) {
	defFile := "/repo/x.go"
	refs := []query.RefRow{{FilePath: defFile}, {FilePath: defFile}}
	sites, pkgs := seamFanIn(refs, defFile, map[string]string{"/repo/x.go": "x"})
	if sites != 0 || pkgs != 0 {
		t.Errorf("sites=%d pkgs=%d, want 0,0 when every ref is the def file", sites, pkgs)
	}
}

// rankSeams must drop non-seams: zero implementations, or fan-in below the
// "touched by only 1 caller isn't a seam" threshold.
func TestRankSeams_FiltersNonSeams(t *testing.T) {
	seams := []Seam{
		{Interface: seamInterface{Name: "NoImpls"}, Impls: nil, FanInDeg: 5, FanInPkgs: 3},
		{Interface: seamInterface{Name: "OneCaller"}, Impls: []query.SymbolRow{{Name: "T"}}, FanInDeg: 1, FanInPkgs: 1},
		{Interface: seamInterface{Name: "RealSeam"}, Impls: []query.SymbolRow{{Name: "T"}}, FanInDeg: 2, FanInPkgs: 2},
	}

	got := rankSeams(seams)
	if len(got) != 1 {
		t.Fatalf("got %d seams, want 1 (only RealSeam clears both bars): %+v", len(got), got)
	}
	if got[0].Interface.Name != "RealSeam" {
		t.Errorf("got %q, want RealSeam", got[0].Interface.Name)
	}
}

// rankSeams must sort by score descending, breaking ties by name ascending
// so output is deterministic across runs.
func TestRankSeams_SortsByScoreThenName(t *testing.T) {
	mk := func(name string, impls int, fanInPkgs, fanInDeg int) Seam {
		is := make([]query.SymbolRow, impls)
		for i := range is {
			is[i] = query.SymbolRow{Name: "Impl"}
		}
		return Seam{Interface: seamInterface{Name: name}, Impls: is, FanInPkgs: fanInPkgs, FanInDeg: fanInDeg}
	}

	seams := []Seam{
		mk("Zebra", 1, 5, 2),  // score = 5*2+1 = 11
		mk("Alpha", 1, 5, 2),  // score = 11, ties with Zebra -> name breaks tie
		mk("Middle", 2, 3, 2), // score = 3*2+2 = 8
	}

	got := rankSeams(seams)
	want := []string{"Alpha", "Zebra", "Middle"}
	if len(got) != len(want) {
		t.Fatalf("got %d seams, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Interface.Name != name {
			t.Errorf("position %d: got %q, want %q (full order: %v)", i, got[i].Interface.Name, name, gotNames(got))
		}
	}
}

func TestRankSeams_ScorePopulated(t *testing.T) {
	seams := []Seam{
		{Interface: seamInterface{Name: "X"}, Impls: []query.SymbolRow{{Name: "T"}}, FanInPkgs: 4, FanInDeg: 3},
	}
	got := rankSeams(seams)
	if len(got) != 1 {
		t.Fatalf("got %d seams, want 1", len(got))
	}
	if want := scoreSeam(4, 1); got[0].Score != want {
		t.Errorf("Score = %d, want %d", got[0].Score, want)
	}
}

func TestRankSeams_EmptyInput(t *testing.T) {
	got := rankSeams(nil)
	if len(got) != 0 {
		t.Errorf("got %d seams from nil input, want 0", len(got))
	}
}

func gotNames(seams []Seam) []string {
	out := make([]string, len(seams))
	for i, s := range seams {
		out[i] = s.Interface.Name
	}
	return out
}
