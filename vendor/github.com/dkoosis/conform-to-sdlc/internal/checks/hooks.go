package checks

import (
	"fmt"
	"os"
	"path/filepath"
)

// Fleet-canonical git-hook shape is B (decision c9ab7db0d7ac, ratified
// 2026-08-10): core.hooksPath=.githooks, a TRACKED repo directory whose
// hooks are source files delegating to bd's machinery. Shape A (.beads/hooks
// as the hook path — tool-generated, usually gitignored, invisible to
// review, empty on fresh clone) is retired. There is ONE predicate; no dual
// shape is accepted.
//
// Surface 1 checks the repo-file half: .githooks exists with executable
// pre-commit and pre-push (the gate carriers). Whether core.hooksPath
// actually points at it is machine state — that half belongs to --local.
const hooksDir = ".githooks"

var requiredHooks = []string{"pre-commit", "pre-push"}

// checkHooksShape verifies the tracked shape-B hook directory (hooks-shape).
func checkHooksShape(dir string) []Finding {
	info, err := os.Stat(filepath.Join(dir, hooksDir))
	if err != nil || !info.IsDir() {
		return []Finding{{
			File:   hooksDir,
			Rule:   RuleHooksShape,
			Msg:    "no tracked .githooks directory — shape B is the one accepted hook shape; untracked hooks are invisible to review and empty on a fresh clone",
			Repair: "track hook sources in .githooks/ and set core.hooksPath=.githooks (shape A, .beads/hooks as hook path, is retired)",
		}}
	}

	var findings []Finding
	for _, h := range requiredHooks {
		rel := filepath.Join(hooksDir, h)
		fi, err := os.Stat(filepath.Join(dir, rel))
		switch {
		case err != nil:
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleHooksShape,
				Msg:    h + " hook missing — the gate it carries silently never fires",
				Repair: fmt.Sprintf("add %s delegating to bd's hook machinery", rel),
			})
		case fi.Mode()&0o111 == 0:
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleHooksShape,
				Msg:    "hook is not executable — git skips it without a word",
				Repair: "chmod +x " + rel,
			})
		}
	}
	return findings
}
