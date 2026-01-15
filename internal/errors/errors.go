// Package errors provides sentinel errors for snipe operations.
package errors

import "errors"

// Sentinel errors for common conditions.
var (
	// ErrSymbolNotFound indicates the requested symbol does not exist in the index.
	ErrSymbolNotFound = errors.New("symbol not found")

	// ErrAmbiguous indicates multiple symbols matched a query.
	ErrAmbiguous = errors.New("ambiguous symbol: multiple matches found")

	// ErrIndexMissing indicates no index exists for the repository.
	ErrIndexMissing = errors.New("index not found")

	// ErrIndexStale indicates the index is out of date with the source.
	ErrIndexStale = errors.New("index is stale")

	// ErrInvalidPosition indicates a malformed file:line:col position.
	ErrInvalidPosition = errors.New("invalid position format")

	// ErrInvalidSymbolID indicates a malformed symbol ID.
	ErrInvalidSymbolID = errors.New("invalid symbol ID format")
)
