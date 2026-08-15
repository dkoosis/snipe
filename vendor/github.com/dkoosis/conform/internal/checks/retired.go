package checks

import (
	"fmt"
	"os"
	"path/filepath"
)

// retiredFiles are per-repo files whose job conform absorbed or whose
// existence is a live footgun. Presence is a finding; the gate stays red
// until the file is gone (retired-files).
//
//   - check-pack-drift.sh soft-failed by design (private upstream → guaranteed
//     no-op in 7 of 8 repos). A rule that cannot fail is worse than an absent
//     rule; drift-guarding the bugclasses pack is conform's job now.
//   - scaffold-codex-review.sh emits an auto-fire pull_request trigger — a
//     re-run is surprise OpenAI spend (the deployed workflows are
//     issue_comment). The codex-workflow rule owns the shape; the scaffolder
//     must not exist where it can be re-run.
var retiredFiles = []string{
	"scripts/check-pack-drift.sh",
	"scripts/scaffold-codex-review.sh",
}

// checkRetiredFiles reports any retired file still present.
func checkRetiredFiles(dir string) []Finding {
	var findings []Finding
	for _, rel := range retiredFiles {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleRetiredFiles,
				Msg:    "retired fleet-wide — its job moved into conform; a soft-fail gate left in place reads as protection and provides none",
				Repair: fmt.Sprintf("git rm %s (and drop any Makefile/CI references)", rel),
			})
		}
	}
	return findings
}
