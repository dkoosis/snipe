package util

import (
	"os"
	"path/filepath"
)

// FindProjectRoot walks up from start looking for a .git directory,
// then falls back to go.mod. Returns "" if neither is found.
// start is resolved to an absolute path first to ensure correct termination.
func FindProjectRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return ""
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
