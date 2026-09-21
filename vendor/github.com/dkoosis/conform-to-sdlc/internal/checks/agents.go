package checks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// AgentsFile is the root file that exists for the harnesses that read nothing
// else. Claude Code reads CLAUDE.md and never AGENTS.md
// (code.claude.com/docs/en/memory.md), so this file is a pointer for Codex and
// its kin and carries no instructions of its own (decision, dk 2026-09-08,
// recorded in home/rules/standard-sdlc.md, which quotes the permitted body
// verbatim).
const AgentsFile = "AGENTS.md"

// agentsLineCap is the ceiling on AGENTS.md's body. The permitted stub is
// seven lines; the cap leaves room for a repo that adds a sentence naming its
// own harness without inviting a second instruction set. It is a shape check,
// not a diff against a golden file: a repo whose pointer says the same thing
// in different words passes, and one that has grown a manual passes only until
// it grows past this.
//
// Blank lines count. Measured 2026-09-08, the case this exists for: canapay's
// AGENTS.md was 119 lines, of which 66-119 were a bd-injected block.
const agentsLineCap = 15

// managedBlockOpen is how a tool marks a region it owns and will rewrite. bd's
// is `<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal -->`; the convention is
// shared, so the detector matches the shape rather than one vendor's spelling.
//
// ‡ Blind spot, named because a check owes one: a tool that injects without
// markers is caught only by agentsLineCap, and a marked block under the cap is
// still caught here — the asymmetry we want, since a marked block regenerates
// after a hand cleanup and a hand-written paragraph does not.
const managedBlockOpen = "<!-- BEGIN"

// checkAgentsStub reports a root AGENTS.md that has grown past a pointer
// (agents-stub).
//
// Absent is not a finding. Three of eleven fleet repos carry no AGENTS.md at
// all (ferret, loto, mnemd, measured 2026-09-08) and nothing is wrong with
// them: the file earns its place only where a harness that ignores
// .claude/rules/ is in use. This rule guards against the file becoming
// content, not against its absence — the opposite direction from checkReadme,
// which requires its file.
//
// Why an enforcing rule and not a one-time cleanup: bd re-injects its block on
// some commands, so a swept repo regresses silently. A check is the only form
// of that decision that survives the next tool deciding the root file is a
// good place to write.
func checkAgentsStub(dir string) []Finding {
	data, err := os.ReadFile(filepath.Join(dir, AgentsFile))
	if err != nil {
		return nil
	}
	body := string(data)

	// The managed block is reported first and on its own: its repair is
	// deletion, and pairing it with a line-count complaint would invite the
	// wrong fix — folding the block into .claude/rules/ preserves the content
	// the decision says must not exist here.
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), managedBlockOpen) {
			return []Finding{{
				File:   AgentsFile,
				Rule:   RuleAgentsStub,
				Msg:    "a tool-managed block in " + AgentsFile + " — the root pointer carries no instructions, and a marked block regenerates after any hand cleanup",
				Repair: "delete the block and its END marker (do not fold it into .claude/rules/); the permitted body is quoted verbatim in home/rules/standard-sdlc.md",
			}}
		}
	}

	if n := len(strings.Split(strings.TrimRight(body, "\n"), "\n")); n > agentsLineCap {
		return []Finding{{
			File:   AgentsFile,
			Rule:   RuleAgentsStub,
			Msg:    AgentsFile + " is " + strconv.Itoa(n) + " lines — past the " + strconv.Itoa(agentsLineCap) + "-line cap, so the file nobody reads is becoming a second instruction set",
			Repair: "reduce it to the stub pointer quoted verbatim in home/rules/standard-sdlc.md; the instructions belong in .claude/rules/",
		}}
	}
	return nil
}
