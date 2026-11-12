package cmd

import "errors"

// Sentinel errors. Callers branch on them with errors.Is; the call sites add
// the specifics with fmt.Errorf and %w.
var (
	// errInvalidArgument is returned for: invalid argument.
	errInvalidArgument = errors.New("invalid argument")
	// errNotFound is returned for: not found.
	errNotFound = errors.New("not found")
	// errGuardViolation is returned for: boundary violation.
	errGuardViolation = errors.New("boundary violation")
	// errNotGitRepo is returned for: not in a git repository.
	errNotGitRepo = errors.New("not in a git repository")
	// errD2Missing is returned for: d2 CLI not found in PATH (install from https://d2lang.com); falling back: rerun without --format=svg to get D2 source.
	errD2Missing = errors.New("d2 CLI not found in PATH (install from https://d2lang.com); falling back: rerun without --format=svg to get D2 source")
	// errAmbiguousType is returned for: ambiguous type — use a more specific name.
	errAmbiguousType = errors.New("ambiguous type — use a more specific name")
	// errNoOutputFile is returned for: no output file available.
	errNoOutputFile = errors.New("no output file available")
	// errIndexMismatch is returned for: index root mismatch.
	errIndexMismatch = errors.New("index root mismatch")
	// errIndexInProgress is returned for: indexing in progress.
	errIndexInProgress = errors.New("indexing in progress")
	// errIndexMissing is returned for: index missing.
	errIndexMissing = errors.New("index missing")
	// errIndexMoved is returned for: index root moved.
	errIndexMoved = errors.New("index root moved")
)
