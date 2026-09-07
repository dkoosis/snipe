package checks

import (
	"fmt"
	"os"
	"path/filepath"
)

// Fix applies the Surface-1 repairs conform can make without judgment, and
// returns one line per action taken. A clean repo returns nothing.
//
// The bar for living here is narrow on purpose: the repair must be the ONLY
// correct one, and it must never destroy work. Creating a file that is absent
// qualifies. Rewriting one that exists does not — conform would be guessing at
// content a human wrote, and a checker that edits your prose stops being
// trusted long before it stops being right. Everything else stays a Repair
// string on the finding, for a person to run.
//
// Fix never overwrites, so running it twice is safe and the second run is
// silent.
func Fix(dir string) ([]string, error) {
	var done []string

	for _, fix := range []func(string) (string, error){fixReadme, fixRoadmap} {
		created, err := fix(dir)
		if err != nil {
			return done, err
		}
		if created != "" {
			done = append(done, created)
		}
	}

	return done, nil
}

// fixRoadmap writes a docs/ROADMAP.md skeleton when the repo has no epic inventory.
//
// A NORTH_STAR.md sitting beside it does not stop this. The two coexist by
// design — the kg's page is the source of direction and this one mirrors its ★
// line over an epic list — so writing the skeleton is right even then, and the
// finding's repair says to copy the ★ line across.
func fixRoadmap(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(abs, RoadmapFile)
	if _, err := os.Stat(path); err == nil {
		return "", nil // present — its ★ line is the checker's business, not ours
	} else if !os.IsNotExist(err) {
		return "", err
	}
	body := RoadmapSkeleton(filepath.Base(abs))
	// docs/ may not exist yet in a repo that never had a direction home.
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	// 0o600: the file is world-readable the moment git tracks it, so a wider
	// mode here buys nothing and trips gosec. #nosec is a worse answer than a
	// mode that is simply correct.
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("created %s — copy the ★ line from the kg's %s, then list the epics", RoadmapFile, NorthStarFile), nil
}

// fixReadme writes a README.md skeleton when the repo root carries none.
//
// The skeleton deliberately does not satisfy the readme rule: its first line
// with content is a comment, so the repo stays red until a person writes the
// heading. A --fix that turned the gate green would be reporting that someone
// had introduced the repo when nobody had.
func fixReadme(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(abs, ReadmeFile)
	if _, err := os.Stat(path); err == nil {
		return "", nil // present — its heading and prose are a person's business
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// 0o600 for the same reason fixRoadmap uses it: git makes the file
	// world-readable the moment it tracks it, so a wider mode buys nothing.
	if err := os.WriteFile(path, []byte(ReadmeSkeleton(filepath.Base(abs))), 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("created %s — replace the opening comment with `# %s`, then say what the repo is", ReadmeFile, filepath.Base(abs)), nil
}
