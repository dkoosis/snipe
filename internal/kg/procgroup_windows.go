//go:build windows

package kg

import "os/exec"

// setProcGroup is a no-op on Windows — process groups are POSIX-specific;
// the orca subprocess this guards against (a hanging shell shim) is a
// POSIX-only test fixture (see hints_test.go's windows skip).
func setProcGroup(_ *exec.Cmd) {}

// killProcGroup falls back to killing just the direct child, matching
// exec.CommandContext's own default Cancel behavior.
func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
