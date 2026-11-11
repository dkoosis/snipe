package edit

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrInvalidRequest is returned for: invalid edit request.
	ErrInvalidRequest = errors.New("invalid edit request")
)
