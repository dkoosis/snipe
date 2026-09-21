package checks

import (
	"os"
	"path/filepath"
)

// VocabularyFile is where a repo's own domain words live — never
// CONTEXT.md (dk 2026-09-10) and never sdlc's process vocabulary, which is a
// separate standard (decision c2d6dc1f9766).
const VocabularyFile = ".claude/rules/vocabulary.md"

// checkVocabulary verifies the repo carries .claude/rules/vocabulary.md
// (vocabulary-file).
//
// Presence only: this rule never opens the file. Judging content — whether
// the words are right, complete, or even present past the file existing — is
// dk's call, not a lint. An empty file passes; the rule's whole job is
// making sure the repo has a home for its domain words, not that anyone has
// filled it in yet.
func checkVocabulary(dir string) []Finding {
	if _, err := os.Stat(filepath.Join(dir, VocabularyFile)); err != nil {
		return []Finding{{
			File:   VocabularyFile,
			Rule:   RuleVocabulary,
			Msg:    "no " + VocabularyFile + " — the repo's own domain words have no home",
			Repair: "create " + VocabularyFile,
		}}
	}
	return nil
}
