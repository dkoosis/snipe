package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/dkoosis/snipe/internal/protocol"
)

// TestClassifySearchErr guards the rg-binary-missing vs runtime-error split.
// A missing binary is detected by the typed exec.ErrNotFound probe, never by
// substring-matching the message — so a runtime error whose text happens to
// contain "not found" is no longer misclassified as RG_NOT_FOUND (snipe-7rn).
func TestClassifySearchErr(t *testing.T) {
	rgMissing := errFixture1search
	runtimeErr := errFixture2search

	tests := []struct {
		name    string
		err     error
		lookErr error
		want    string
	}{
		{
			name:    "no error yields empty code",
			err:     nil,
			lookErr: nil,
			want:    "",
		},
		{
			name:    "missing rg binary routes to RG_NOT_FOUND",
			err:     rgMissing,
			lookErr: exec.ErrNotFound,
			want:    protocol.ErrRgNotFound,
		},
		{
			name:    "wrapped exec.ErrNotFound routes to RG_NOT_FOUND",
			err:     rgMissing,
			lookErr: fmt.Errorf("exec %q: %w", "rg", exec.ErrNotFound),
			want:    protocol.ErrRgNotFound,
		},
		{
			name:    "runtime error with rg present routes to INTERNAL_ERROR",
			err:     runtimeErr,
			lookErr: nil,
			want:    protocol.ErrInternal,
		},
		{
			// Regression: message contains "not found" but rg is present —
			// the old substring match wrongly tagged this RG_NOT_FOUND.
			name:    "non-rg error containing 'not found' stays INTERNAL_ERROR",
			err:     errFixture3search,
			lookErr: nil,
			want:    protocol.ErrInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySearchErr(tt.err, tt.lookErr); got != tt.want {
				t.Errorf("classifySearchErr(%v, %v) = %q, want %q", tt.err, tt.lookErr, got, tt.want)
			}
		})
	}
}

// Fixture errors: sentinels so the tests stay clear of dynamic errors.
var (
	errFixture1search = errors.New("ripgrep (rg) not found: install from the project page")
	errFixture2search = errors.New("rg error (exit 2): regex parse error")
	errFixture3search = errors.New("pattern not found in any tracked file")
)
