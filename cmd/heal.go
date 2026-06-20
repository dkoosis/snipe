package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

// healMaxChangedFiles bounds inline self-healing: drift larger than this is
// reported, not repaired, so a query never blocks on a long reindex.
const healMaxChangedFiles = 20

// healTimeout caps the self-heal subprocess. Incremental indexing of a few
// files finishes well under this; if it doesn't, the query proceeds stale.
const healTimeout = 15 * time.Second

// maybeSelfHeal runs an incremental reindex when the index has drifted by a
// small number of files. Queries against a stale index silently lie (missing
// symbols, wrong lines), which costs more trust than the ~1s heal. Returns
// true if a heal ran (caller should reopen the store).
//
// Heals only when the build fingerprint matches (incremental-shaped drift);
// config or version changes need a full reindex and only get the existing
// stale warning. Set SNIPE_NO_HEAL=1 to disable.
func maybeSelfHeal(s *store.Store, root string) bool {
	if os.Getenv("SNIPE_NO_HEAL") != "" {
		return false
	}

	// Use the root the index was built with: a freshly-resolved root can
	// differ in symlink canonicalization (macOS /var vs /private/var), which
	// makes every stored file look added+deleted — phantom drift that would
	// trigger a heal rewriting all paths and breaking path-based queries.
	if storedRoot, err := s.GetMeta("repo_root"); err == nil && storedRoot != "" {
		root = storedRoot
	}

	fp, err := index.ComputeFingerprint(root, Version)
	if err != nil {
		return false
	}
	storedFP, err := s.GetMeta("fingerprint")
	if err != nil || storedFP != fp.Combined {
		return false
	}

	storedFiles, err := s.GetAllFiles()
	if err != nil || len(storedFiles) == 0 {
		return false
	}
	changes, err := index.DetectChanges(root, storedFiles, index.DefaultExclude())
	if err != nil || !changes.HasChanges {
		return false
	}
	if !shouldHeal(changes.TotalChanged()) {
		return false
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}

	// Honor the command context (--timeout / SIGINT) so a heal can never block a
	// query past its caller's wall-clock budget. CommandContext also kills the
	// child when ctx fires; runHealCmd's select adds the 15s backstop.
	ctx := GetContext()
	cmd := exec.CommandContext(ctx, exe, "index", "--embed-mode=off", "--enrich=false", root)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if !runHealCmd(ctx, cmd) {
		return false
	}

	fmt.Fprintf(os.Stderr, "healed index: %d changed files reindexed\n", changes.TotalChanged())
	return true
}

// runHealCmd starts cmd and waits for it under three exit conditions: clean
// finish (true), ctx cancellation (kill + drain, false), or the healTimeout
// backstop (kill + drain, false). It is the testable seam for the ctx-honoring
// behavior — maybeSelfHeal's store/fingerprint preamble makes it hard to drive
// end to end, so the subprocess lifecycle lives here on its own.
func runHealCmd(ctx context.Context, cmd *exec.Cmd) bool {
	if err := cmd.Start(); err != nil {
		return false
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return false
	case <-time.After(healTimeout):
		_ = cmd.Process.Kill()
		<-done
		return false
	}
}

// shouldHeal is the inline-heal decision: small drift heals, large drift warns.
func shouldHeal(changedFiles int) bool {
	return changedFiles > 0 && changedFiles <= healMaxChangedFiles
}
