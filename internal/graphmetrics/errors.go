package graphmetrics

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// ErrNoModuleDirective is returned for: module directive not found.
	ErrNoModuleDirective = errors.New("module directive not found")
	// ErrNoModulePath is returned for: could not determine module path for intra-repo filter.
	ErrNoModulePath = errors.New("could not determine module path for intra-repo filter")
)
