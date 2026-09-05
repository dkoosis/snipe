package checks

import (
	"os"
	"path/filepath"
	"strings"
)

// RoadmapFile is the repo's epic inventory: the ordered epics, each naming its
// bd id, under a ★ line MIRRORED from the kg. It lives under docs/: the root is
// minimal and a root entry earns its place (decision d9cd0e20868b, dk
// 2026-09-02) — README.md is the one file the root must carry; direction
// documents sit under docs/. A ROADMAP.md at the root is a root-minimal
// finding, never a second place this rule looks.
//
// The kg holds direction, the repo holds the queue (home/rules/sdlc.md §The
// standard; dk 2026-08-19, decision 538e0c94b65b, which reversed the earlier
// "direction belongs beside the code" call). dk edits
// Project/<repo>/NORTH_STAR.md and nothing else is a source; the ★ line here
// is a copy that exists so the session-open path can grep one file beside the
// code. Copy, never owner — which is why this rule checks that the line is
// PRESENT and never that it is right.
const RoadmapFile = "docs/ROADMAP.md"

// starPrefix marks the destination sentence. Every renderer that shows "where
// is this project going" greps for exactly this, at the start of a line.
const starPrefix = "★"

// NorthStarFile is the kg's direction document, which a repo may also carry as
// a Publish-To reflection. It is NOT a retired name and never a rename target:
// decision 9b4cbc91016f settles the name as final, and sdlc.md makes it the one
// source. A repo holding it is holding a generated copy of dk's page — renaming
// that away would delete the copy AND orphan the publish target, so the next
// publish would re-create it and fight this checker forever.
//
// Its presence is never a finding. When ROADMAP.md is absent and this is here,
// the repair is to WRITE a roadmap mirroring its ★ line — not to move anything.
const NorthStarFile = "docs/NORTH_STAR.md"

// checkRoadmap verifies the repo carries a docs/ROADMAP.md with a ★ destination
// line (roadmap).
//
// Two failures, deliberately distinct. A missing file means the repo has no
// direction home at all. A file without a ★ line is worse in one specific way:
// every reader — the session-open banner, /journey, this checker — greps for
// that line, so the file exists and answers nothing, which reads as "the
// renderer is broken" rather than "the page is unfinished".
//
// What is NOT checked: milestones, epic ids, ordering, progress. Progress
// never belongs here — it derives from the bd DAG at read time — and a
// milestone list is a judgment a checker cannot make. This rule guards the
// artifact's existence and its one machine-read line; the rest is dk's.
func checkRoadmap(dir string) []Finding {
	data, err := os.ReadFile(filepath.Join(dir, RoadmapFile))
	if err != nil {
		return []Finding{{
			File:   RoadmapFile,
			Rule:   RuleRoadmap,
			Msg:    "no direction home — nothing in the repo says where this project is going",
			Repair: roadmapRepair(dir),
		}}
	}
	if !hasStarLine(string(data)) {
		return []Finding{{
			File:   RoadmapFile,
			Rule:   RuleRoadmap,
			Msg:    "no ★ line — every reader greps for it, so the file exists and answers nothing",
			Repair: `add a line starting "★ " with the destination in one sentence`,
		}}
	}
	return nil
}

// hasStarLine reports whether any line starts with the ★ marker. Leading
// whitespace does not count: readers anchor the grep at the line start, so a
// star that only a human can see is not the line they are looking for.
func hasStarLine(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, starPrefix) {
			return true
		}
	}
	return false
}

// roadmapRepair names the cheapest true fix. A repo already carrying the
// published NORTH_STAR.md has the ★ line on disk, so the repair is to copy that
// one line across — the two files coexist by design and neither replaces the
// other.
func roadmapRepair(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, NorthStarFile)); err == nil {
		return "conform --fix, then copy the ★ line from " + NorthStarFile + " into " + RoadmapFile
	}
	return "conform --fix (writes a " + RoadmapFile + " skeleton to fill in)"
}

// RoadmapSkeleton renders a starting ROADMAP.md for `conform --fix` to drop
// into an existing repo. Every line is a prompt, and the ★ placeholder is
// deliberately unusable as-is, so a repo that runs --fix and stops still fails
// the ★ check and says why, rather than passing with a file nobody wrote.
func RoadmapSkeleton(repo string) string {
	return roadmapDoc(repo, roadmapFixDestination)
}

// RoadmapScaffold renders the same page for `conform init`, which promises a
// repo that passes the checker unedited — so this one carries a real ★ line.
//
// The two differ on exactly one block, and the difference is who is standing
// there. --fix repairs a repo that already exists and may be unattended, so a
// green gate would be a lie about a page nobody wrote. init is a person naming
// a new repo at the keyboard, watching conform list what it emitted; the ★
// line it gets is visibly a prompt, and the promise it keeps — scaffold, run
// the gate, see green — is what makes the scaffold trustworthy at all.
//
// One body, two callers, so the page a scaffold emits and the shape the
// checker demands cannot drift.
func RoadmapScaffold(repo string) string {
	return roadmapDoc(repo, roadmapInitDestination)
}

// roadmapFixDestination refuses to satisfy hasStarLine: no line starts with ★.
const roadmapFixDestination = "<!-- Replace this comment with the ★ line from the kg's\n" +
	"     Project/<repo>/NORTH_STAR.md, copied verbatim, at the start of a line.\n" +
	"     Until you do, `conform` stays red on the roadmap rule — a skeleton\n" +
	"     that passed the gate would read as direction and carry none. -->"

// roadmapInitDestination satisfies hasStarLine and reads as unfinished, which
// is the honest state of a repo one command old.
const roadmapInitDestination = "★ <mirror the ★ line from the kg: Project/<repo>/NORTH_STAR.md>"

// roadmapDoc renders the page around whichever destination block the caller
// wants. Everything below the destination is identical for both.
func roadmapDoc(repo, destination string) string {
	return "# " + repo + `

` + destination + `

<A paragraph on what this repo is. Direction is not decided here: dk edits
Project/<repo>/NORTH_STAR.md in the kg and nothing else is a source, so the ★
line above is a copy kept beside the code for the session-open path. What this
file owns is the epic inventory below — what is done, what is underway, what
comes next. Hand-written; agents do not edit it unprompted.>

## Epics

Ordered, one line per epic. Progress is never written here — it derives at read
time from the bd DAG joined against these ids.

1. [status] <epic title> → <bd-epic-id>

## Non-goals

- <what this project deliberately will not do>

## Resources

- <one hop to every resource the project has>
`
}
