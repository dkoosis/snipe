package store

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrLocked is returned for: index is locked by another process.
	ErrLocked = errors.New("index is locked by another process")
	// ErrPragmaMismatch is returned for: pragma mismatch.
	ErrPragmaMismatch = errors.New("pragma mismatch")
)
