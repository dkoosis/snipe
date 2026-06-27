package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dkoosis/snipe/internal/edit"
	"github.com/dkoosis/snipe/internal/output"
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
			want: output.ErrNotFound,
		},
		{
			name: "wrapped sentinel still routes to NOT_FOUND",
			err:  fmt.Errorf("%w: %q in foo.go", edit.ErrSymbolNotFound, "Bar"),
			want: output.ErrNotFound,
		},
		{
			name: "generic failure routes to INTERNAL_ERROR",
			err:  errors.New("write: disk full"),
			want: output.ErrInternal,
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
