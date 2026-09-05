package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dkoosis/conform/internal/values"
)

// The four-verb contract: check · audit · deploy · help, identical in every
// repo; internals are compositions. Only the top-level Makefile is parsed —
// targets provided by .sandbox includes (doctor, cross*) are theirs to
// document.
var (
	// checkFloor is what `check` must compose, at minimum. Extra prereqs are
	// per-repo freedom.
	checkFloor = []string{"vet", "lint", "test", "build"}

	// targetLine matches a rule head at column 0: name, colon, and NOT an
	// assignment (`X := y`) or a double-colon assignment. Dot-targets
	// (.PHONY, .DEFAULT_GOAL) and pattern rules (%) are not contract
	// targets.
	targetLine = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*:($|[^=].*)`)
)

type mkTarget struct {
	name    string
	prereqs []string
	doc     bool // carries a "##" help comment
}

// parseMakefile extracts contract-relevant targets from Makefile text.
func parseMakefile(text string) []mkTarget {
	var targets []mkTarget
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			continue // recipe or comment
		}
		m := targetLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := m[2]
		doc := strings.Contains(rest, "##")
		if i := strings.Index(rest, "#"); i >= 0 {
			rest = rest[:i]
		}
		targets = append(targets, mkTarget{
			name:    m[1],
			prereqs: strings.Fields(rest),
			doc:     doc,
		})
	}
	return targets
}

// contractVerbs is the human surface: the same words in every repo.
var contractVerbs = []string{"check", "audit", "help"}

// requiredVerbs is the verb list for a profile — `deploy` is
// profile-dependent (tools ship it, libs must not have it; no-deploy is what
// the lib profile means). verbFindings checks against this list and the
// scaffold renderer emits from it, so the two cannot disagree.
func requiredVerbs(profile values.Profile) []string {
	verbs := append([]string{}, contractVerbs...)
	if profile == values.ProfileTool {
		verbs = append(verbs, "deploy")
	}
	return verbs
}

// checkMakefile enforces the verb contract (makefile-verbs) and ## doc
// coverage (makefile-docs) on the top-level Makefile.
func checkMakefile(dir string, profile values.Profile) []Finding {
	const file = "Makefile"
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return []Finding{{
			File:   file,
			Rule:   RuleMakefileVerb,
			Msg:    "no top-level Makefile — the four-verb contract (check · audit · deploy · help) has no home",
			Repair: "copy the reference Makefile from ferret and adapt targets",
		}}
	}

	targets := parseMakefile(string(data))
	byName := make(map[string]mkTarget, len(targets))
	for _, t := range targets {
		if _, dup := byName[t.name]; !dup {
			byName[t.name] = t
		}
	}

	findings := verbFindings(byName, profile)
	findings = append(findings, docFindings(targets)...)
	return findings
}

// verbFindings enforces verb presence and prereq composition
// (makefile-verbs). `deploy` is profile-dependent: tools ship it, libs must
// not have it (no-deploy is what the lib profile means).
func verbFindings(byName map[string]mkTarget, profile values.Profile) []Finding {
	var findings []Finding
	add := func(msg, repair string) {
		findings = append(findings, Finding{File: "Makefile", Rule: RuleMakefileVerb, Msg: msg, Repair: repair})
	}

	for _, verb := range requiredVerbs(profile) {
		if _, ok := byName[verb]; !ok {
			add(
				fmt.Sprintf("verb %q missing — the human surface is four verbs, identical in every repo", verb),
				fmt.Sprintf("add a %q target composing existing internal steps", verb),
			)
		}
	}
	if _, ok := byName["deploy"]; ok && profile == values.ProfileLib {
		add(
			"lib profile carries a deploy verb — libs are the contract minus deploy",
			"delete the deploy target (or set profile to tool in conform.json)",
		)
	}

	// Prereq composition: check composes the gate; audit runs check first.
	if check, ok := byName["check"]; ok {
		if missing := missingFrom(check.prereqs, checkFloor); len(missing) > 0 {
			add(
				"check does not compose "+strings.Join(missing, ", ")+" — the fast gate must run vet + lint + test + build",
				"add "+strings.Join(missing, " ")+" to check's prerequisites",
			)
		}
	}
	if audit, ok := byName["audit"]; ok {
		if len(missingFrom(audit.prereqs, []string{"check"})) > 0 {
			add(
				"audit does not run check — an audit that skips the fast gate audits nothing",
				"make check audit's first prerequisite",
			)
		}
	}
	return findings
}

// docFindings enforces ## coverage (makefile-docs): every top-level target
// self-documents; `make help`'s awk lister only shows documented targets.
func docFindings(targets []mkTarget) []Finding {
	var undocumented []string
	for _, t := range targets {
		if !t.doc {
			undocumented = append(undocumented, t.name)
		}
	}
	if len(undocumented) == 0 {
		return nil
	}
	sort.Strings(undocumented)
	return []Finding{{
		File:   "Makefile",
		Rule:   RuleMakefileDocs,
		Msg:    "targets without a ## doc comment: " + strings.Join(undocumented, ", ") + " — they vanish from make help",
		Repair: `append "## <one-line purpose>" to each target line`,
	}}
}

// missingFrom returns the members of want absent from have, in want order.
func missingFrom(have, want []string) []string {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	var missing []string
	for _, w := range want {
		if !set[w] {
			missing = append(missing, w)
		}
	}
	return missing
}
