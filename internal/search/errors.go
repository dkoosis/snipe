package search

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrRgFailed is returned for: rg failed.
	ErrRgFailed = errors.New("rg failed")
	// ErrRgMissing is returned for: ripgrep (rg) not found: install from https://github.com/BurntSushi/ripgrep.
	ErrRgMissing = errors.New("ripgrep (rg) not found: install from https://github.com/BurntSushi/ripgrep")
)
