// Package atomicfile writes files so that a crash — a kill, a panic, a power
// loss — can never leave a torn, half-written file behind.
//
// # Why this package exists
//
// A plain os.WriteFile truncates the target and then writes into it. A crash
// between those two steps destroys the old content and leaves the new content
// partial — the file is torn. A 2026 audit of nine Go repositories in this
// fleet found roughly forty shipped defects of exactly this class (decision
// d7ea466a8172): state files written non-atomically, renames that were never
// made durable because the parent directory was not fsynced, and macOS writes
// that never truly reached disk.
//
// The idiom that fixes it is well known — write a temporary file in the same
// directory, fsync it, rename it over the target — and google/renameio
// implements it well. This package is a thin wrapper over renameio adding the
// two guarantees it omits:
//
//   - The parent directory is fsynced after the rename. Without this, the
//     rename itself can vanish on power loss, taking the "atomic" write with
//     it: the data was durable, the directory entry was not.
//   - On macOS, fsync does not force data to stable storage — the kernel may
//     leave it in the drive's volatile cache. F_FULLFSYNC does. This package
//     issues F_FULLFSYNC on darwin, falling back to fsync on filesystems
//     that reject it (some network and external volumes).
//
// When the target already exists, its permissions are preserved (a past
// defect hardcoded 0600 and silently stripped a file's mode); the perm
// argument applies only when the target is new.
//
// # When NOT to use this package
//
// Rename-replace is the wrong primitive for three kinds of files:
//
//   - Lock-files. flock locks an inode; rename swaps the inode. Replacing a
//     locked file breaks mutual exclusion — the holder keeps the old inode
//     while new lockers lock the new one. Edit lock-files in place under
//     their lock (see cmd/go/internal/lockedfile for the pattern).
//   - Live SQLite databases. Open connections keep the old inode, and the
//     -wal/-shm sidecars desync from the swapped file. SQLite owns its own
//     durability (WAL mode, synchronous pragma) — leave it to the pragmas.
//   - Multi-writer read-modify-write files. Atomic replace prevents torn
//     writes, not lost updates: two writers each replacing the whole file
//     silently drop one writer's change. Serialize the read-modify-write (a
//     lock, or re-read and merge) on top of this package.
package atomicfile

import (
	"io"
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
)

// WriteFile atomically and durably replaces the file at path with data.
// perm applies only when path does not yet exist; an existing file keeps its
// permissions. On return with a nil error, the new content — and the
// directory entry pointing at it — have been flushed to stable storage.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return WriteFileFunc(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// WriteFileFunc is WriteFile for streamed content: fn writes the body to w.
// If fn returns an error, the target is untouched and the temporary file is
// cleaned up.
func WriteFileFunc(path string, perm os.FileMode, fn func(w io.Writer) error) error {
	pf, err := renameio.NewPendingFile(path,
		renameio.WithExistingPermissions(),
		renameio.WithPermissions(perm),
	)
	if err != nil {
		return err
	}
	defer pf.Cleanup() //nolint:errcheck // best-effort removal of the temp file on the error paths

	if err := fn(pf); err != nil {
		return err
	}
	// Flush file data to stable storage (F_FULLFSYNC on darwin) before the
	// rename publishes it.
	if err := syncFile(pf.File); err != nil {
		return err
	}
	if err := pf.CloseAtomicallyReplace(); err != nil {
		return err
	}
	// Flush the directory entry: without this the rename itself is not
	// crash-durable.
	return syncDir(filepath.Dir(path))
}

// syncDir fsyncs a directory so a just-renamed entry survives power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read-only fd; the fsync is the operation that matters
	return syncFile(d)
}
