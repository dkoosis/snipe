package checks

import (
	"os"
	"path/filepath"
)

// rootStrays are the entries that must not sit at the repo root (root-minimal).
// Four files the decision names, plus kg/: a vault directory inside a repo is
// never intended — it is what `trixi set` leaves when its --kg-root default
// (./kg) meets a repo cwd (sd-mzgy.6, mnemd and memorybench, 2026-09-02).
//
// The root is minimal and a root entry earns its place (decision d9cd0e20868b,
// dk 2026-09-02): README.md is the one file the root must carry, direction
// documents live under docs/, and .claude/rules/** is the whole project
// instruction set — there is no CLAUDE.md. A dotfile stays at the root only
// when its host tool reads it there by name (AGENTS.md, .golangci.yml, go.mod).
//
// This is a deny-list, not an allowlist
// of everything a root may hold — the latter is a judgment nobody has made, and
// a checker that flagged LICENSE or tools.go would be guessing. The test the
// decision gives is GitHub's repo page: the README's first paragraph visible
// without scrolling.
var rootStrays = []struct{ name, msg, repair string }{
	{
		name:   "CLAUDE.md",
		msg:    "no CLAUDE.md — .claude/rules/** is the whole project instruction set, loaded at launch with the same priority and scoped where CLAUDE.md cannot be",
		repair: "fold anything load-bearing into .claude/rules/<topic>.md, then git rm CLAUDE.md",
	},
	{
		name:   "ROADMAP.md",
		msg:    "direction documents live under docs/ — the root is minimal",
		repair: "git mv ROADMAP.md " + RoadmapFile,
	},
	{
		name:   "conform.json",
		msg:    "repo-level declarations live under docs/ — the root is minimal",
		repair: "git mv conform.json " + ValuesFile,
	},
	{
		name:   "NORTH_STAR.md",
		msg:    "a Publish-To reflection of the kg's page lives under docs/ — the root is minimal",
		repair: "git mv NORTH_STAR.md " + NorthStarFile + " (and repoint the publish target)",
	},
	{
		name:   "kg",
		msg:    "a vault inside the repo — a nug writer defaulted its root to ./kg (trixi set) instead of ~/Projects/kg",
		repair: "move the nugs under ~/Projects/kg (itzy nug re-files them), rm -r kg, and set TRIXI_KG / use itzy nug",
	},
}

// checkRootMinimal reports each rootStray present at the repo root.
func checkRootMinimal(dir string) []Finding {
	var findings []Finding
	for _, s := range rootStrays {
		if _, err := os.Stat(filepath.Join(dir, s.name)); err == nil {
			findings = append(findings, Finding{
				File:   s.name,
				Rule:   RuleRootMinimal,
				Msg:    s.msg,
				Repair: s.repair,
			})
		}
	}
	return findings
}
