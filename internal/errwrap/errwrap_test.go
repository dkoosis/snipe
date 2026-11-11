package errwrap_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/dkoosis/snipe/internal/errwrap"
)

func TestWrap_ReturnsNil_When_ErrIsNil(t *testing.T) {
	t.Parallel()
	if got := errwrap.Wrap(nil, "x"); got != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", got)
	}
}

func TestWrap_KeepsTheCause_When_ErrIsSet(t *testing.T) {
	t.Parallel()
	got := errwrap.Wrap(fs.ErrNotExist, "open config")
	if !errors.Is(got, fs.ErrNotExist) {
		t.Fatalf("Wrap lost the cause: %v", got)
	}
	if got.Error() != "open config: file does not exist" {
		t.Fatalf("Wrap message = %q", got.Error())
	}
}
