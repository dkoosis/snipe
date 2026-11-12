package embed

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrAPI is returned for: API error.
	ErrAPI = errors.New("API error")
	// ErrNoEmbedding is returned for: embedding missing.
	ErrNoEmbedding = errors.New("embedding missing")
	// ErrDuplicateID is returned for: duplicate symbol ID.
	ErrDuplicateID = errors.New("duplicate symbol ID")
	// ErrNoAPIKey is returned for: no API key: set VOYAGE_API_KEY or store one in the keychain (security add-generic-password -U -s snipe -a voyage -w KEY).
	ErrNoAPIKey = errors.New("no API key: set VOYAGE_API_KEY or store one in the keychain (security add-generic-password -U -s snipe -a voyage -w KEY)")
	// ErrNoEmbeddingReturned is returned for: no embedding returned.
	ErrNoEmbeddingReturned = errors.New("no embedding returned")
)
