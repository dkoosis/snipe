package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/protocol"
)

// errUsage marks a usage error found in Run rather than by kong: the
// arguments parsed, but they do not fit together (a second symbol at a
// one-symbol depth, an extra positional beside a mode flag, a flag the chosen
// mode ignores). usageError has already written it as an INVALID_ARGS
// envelope; Execute exits 2 for it, the code a kong flag error gets.
var errUsage = errors.New("usage error")

// usageError writes the message as an INVALID_ARGS envelope through the
// output writer, so a --format json caller can parse it, and returns
// errUsage so Execute exits 2.
func usageError(cmdName, format string, args ...any) error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	if err := w.WriteError(cmdName, &protocol.Error{
		Code:    protocol.ErrInvalidArgs,
		Message: fmt.Sprintf(format, args...),
	}); err != nil {
		return err
	}
	return errUsage
}
