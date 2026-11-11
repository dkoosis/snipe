// Package errwrap adds context to an error from another package without a
// four-line if block, so a call that returns only an error can be returned as
// errwrap.Wrap(call(), "what failed").
package errwrap

import "fmt"

// Wrap returns nil when err is nil, otherwise err wrapped with what and %w, so
// errors.Is and errors.As still see err.
func Wrap(err error, what string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
