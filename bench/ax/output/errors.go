package main

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// errListing is returned for: malformed listing.
	errListing = errors.New("malformed listing")
	// errUsage is returned for: bad usage.
	errUsage = errors.New("bad usage")
)
