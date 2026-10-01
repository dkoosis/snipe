package util

import (
	"os"
	"path/filepath"
)

// FindProjectRoot walks up from start looking for a .git directory,
// then falls back to go.mod. Returns "" if neither is found.
// start is resolved to an absolute path first to ensure correct termination.
//
// The resolved path is also canonicalized via EvalSymlinks (sn-za8p). Query
// commands resolve project root from os.Getwd(), which on macOS the kernel
// already returns in canonical form (/private/var/... not /var/...). Index
// commands resolve an explicit `snipe index <path>` arg through this same
// function; without canonicalizing here too, a symlinked start (e.g. a
// /var/folders CI temp dir passed explicitly) would leave the returned root
// in its unresolved form. The two sides would then disagree on file_path
// values and every `s.file_path = ?` store lookup would silently return zero
// rows. EvalSymlinks failing (e.g. path doesn't exist) is not fatal here —
// fall back to the plain absolute path.
func FindProjectRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	// First pass: look for .git
	if root := walkUp(abs, ".git"); root != "" {
		return root
	}
	// Second pass: fall back to go.mod
	return walkUp(abs, "go.mod")
}

// walkUp walks from start toward fs root, returning the first dir containing marker.
func walkUp(start, marker string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// RootMoved reports whether an index built at stored now serves a repo at
// current: the repo was moved or renamed after indexing. Another spelling of
// the same directory is not a move: a symlink (macOS /var vs /private/var) or
// a case difference on a case-insensitive volume (/Users vs /users). An empty
// stored root (an index from before repo_root was recorded) is never a move.
func RootMoved(stored, current string) bool {
	if stored == "" || current == "" {
		return false
	}
	if canonical(stored) == canonical(current) {
		return false
	}
	a, errA := os.Stat(stored)
	b, errB := os.Stat(current)
	if errA == nil && errB == nil {
		return !os.SameFile(a, b)
	}
	return true
}

func canonical(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}
