package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dkoosis/snipe/internal/edit"
	"github.com/dkoosis/snipe/internal/protocol"
)

// TestEditErrCode guards the not-found classification: edit.ErrSymbolNotFound
// is a public sentinel and must surface to Claude as NOT_FOUND, not the
// INTERNAL_ERROR crash code (snipe-7rn, D1).
func TestEditErrCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "bare sentinel routes to NOT_FOUND",
			err:  edit.ErrSymbolNotFound,
			want: protocol.ErrNotFound,
		},
		{
			name: "wrapped sentinel still routes to NOT_FOUND",
			err:  fmt.Errorf("%w: %q in foo.go", edit.ErrSymbolNotFound, "Bar"),
			want: protocol.ErrNotFound,
		},
		{
			name: "generic failure routes to INTERNAL_ERROR",
			err:  errFixture1edit,
			want: protocol.ErrInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := editErrCode(tt.err); got != tt.want {
				t.Errorf("editErrCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// Fixture errors: sentinels so the tests stay clear of dynamic errors.
var (
	errFixture1edit = errors.New("write: disk full")
)
