//go:build darwin

package atomicfile

import (
	"os"

	"golang.org/x/sys/unix"
)

// syncFile forces f to stable storage. On darwin, fsync only reaches the
// drive's volatile cache; F_FULLFSYNC is the real barrier (SQLite does the
// same). Filesystems that reject F_FULLFSYNC (some network/external volumes)
// fall back to plain fsync — weaker, but the strongest available there.
func syncFile(f *os.File) error {
	if _, err := unix.FcntlInt(f.Fd(), unix.F_FULLFSYNC, 0); err == nil {
		return nil
	}
	return f.Sync()
}
