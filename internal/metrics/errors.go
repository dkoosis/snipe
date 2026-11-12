package metrics

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrOverflow is returned for: value overflows int.
	ErrOverflow = errors.New("value overflows int")
)
