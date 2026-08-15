package checks

import (
	"os"
	"path/filepath"
	"strings"
)

// prTemplatePaths are the casings GitHub accepts for a PR template. The
// uppercase form is the fleet-canonical spelling; either passes.
var prTemplatePaths = []string{
	".github/PULL_REQUEST_TEMPLATE.md",
	".github/pull_request_template.md",
}

// checkPRTemplate verifies a non-empty PR template exists (pr-template).
//
// Surface 1 since v0.2.0 — moved from --fleet by the files-here principle:
// the template is a tracked repo file, so it belongs with hooks-shape and
// bd-config in the in-repo surface (the same split that keeps live
// core.hooksPath in --local and GitHub settings in --fleet). It is also the
// named rule change for the sd-th5.22 iteration-loop proof: every repo
// lacking the artifact goes red at the v0.2.0 pin bump.
func checkPRTemplate(dir string) []Finding {
	for _, rel := range prTemplatePaths {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == "" {
			return []Finding{{
				File:   rel,
				Rule:   RulePRTemplate,
				Msg:    "PR template is empty — it structures nothing",
				Repair: "fill " + rel + " (copy the fleet reference from conform)",
			}}
		}
		return nil
	}
	return []Finding{{
		File:   prTemplatePaths[0],
		Rule:   RulePRTemplate,
		Msg:    "no PR template — agent PRs open with free-form bodies",
		Repair: "add " + prTemplatePaths[0] + " (copy the fleet reference from conform)",
	}}
}
