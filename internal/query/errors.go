package query

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrSymbolNotFound is returned for: symbol not found.
	ErrSymbolNotFound = errors.New("symbol not found")
	// ErrUnsupportedKind is returned for: unsupported symbol kind.
	ErrUnsupportedKind = errors.New("unsupported symbol kind")
	// ErrInvalidPosition is returned for: invalid position.
	ErrInvalidPosition = errors.New("invalid position")
	// ErrNilSymbol is returned for: symbol is nil.
	ErrNilSymbol = errors.New("symbol is nil")
)
