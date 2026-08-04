//go:build !windows

package kg

import (
	"errors"
	"os/exec"
	"syscall"
)

// setProcGroup puts cmd in its own process group, so killProcGroup can reach
// any children it forks (a shell shim that execs a long-running child, e.g.)
// instead of only the direct child exec.CommandContext started.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcGroup sends SIGKILL to cmd's whole process group. A bare
// cmd.Process.Kill() only reaches the direct child; if that child is a shell
// that spawned (rather than exec'd into) a hung grandchild, the grandchild
// survives the kill and GetHints blocks until the grandchild exits on its
// own — observed in CI as TestGetHints_HonorsContextCancel running the full
// orca shim's 60s sleep instead of honoring ctx cancel (sn-nw0m).
func killProcGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil // group already gone
	}
	return err
}
