package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	_ "modernc.org/sqlite"
)

// Store manages the SQLite index database
type Store struct {
	db   *sql.DB
	path string
}

// DefaultIndexPath returns the default index path for a repo
func DefaultIndexPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".snipe", "index.db")
}

// Open opens or creates an index database at the given path
func Open(path string) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Close db on any error after this point
	ok := false
	defer func() {
		if !ok {
			_ = db.Close() // G104: cleanup on error path
		}
	}()

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}
	if err := verifyPragmaString(db, "journal_mode", "wal"); err != nil {
		return nil, err
	}

	// Balance durability and latency for WAL workloads
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	if err := verifyPragmaInt(db, "synchronous", 1); err != nil {
		return nil, err
	}

	// Keep temporary data in memory for speed
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		return nil, fmt.Errorf("set temp_store: %w", err)
	}
	if err := verifyPragmaInt(db, "temp_store", 2); err != nil {
		return nil, err
	}

	// Set busy timeout to avoid "database is locked" errors during concurrent access
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if err := verifyPragmaInt(db, "busy_timeout", 5000); err != nil {
		return nil, err
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := verifyPragmaInt(db, "foreign_keys", 1); err != nil {
		return nil, err
	}

	// Limit connections to avoid lock contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: path}

	// Initialize schema
	if err := s.initSchema(); err != nil {
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

	ok = true
	return s, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for advanced queries
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path returns the database file path
func (s *Store) Path() string {
	return s.path
}

// Exists checks if an index database exists at the given path
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LockPath returns the lock file path for an index
func LockPath(dbPath string) string {
	return dbPath + ".lock"
}

// IsIndexing reports whether indexing is in progress: a lock file exists AND its
// holder process is still alive. It is a pure predicate — it performs no
// filesystem mutation, so the read commands that gate on it (cmd/root.go,
// cmd/search.go) stay read-only. A stale lock (dead holder, e.g. an interrupted
// `snipe index`) reports not-indexing but is left on disk; reaping it is a
// separate, explicit step performed on the write path by AcquireLock.
func IsIndexing(dbPath string) bool {
	lockPath := LockPath(dbPath)
	if _, err := os.Stat(lockPath); err != nil {
		return false
	}
	// Lock exists — only "in progress" if the holder is still alive.
	stale, _ := lockHeldByDeadProcess(lockPath)
	return !stale
}

// AcquireLock creates a lock file for indexing.
// The lock file contains the PID of the owning process. If a stale lock is
// detected (owning process no longer running), it is automatically removed.
func AcquireLock(dbPath string) error {
	lockPath := LockPath(dbPath)
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600) // #nosec G304
	if err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("create lock file: %w", err)
		}
		// Lock exists — check if holder is still alive
		if !tryRemoveStaleLock(lockPath) {
			return fmt.Errorf("index is locked by another process (see %s)", lockPath)
		}
		// Stale lock removed, retry once
		f, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600) // #nosec G304
		if err != nil {
			return fmt.Errorf("create lock file after stale removal: %w", err)
		}
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return f.Close()
}

// lockHeldByDeadProcess reports whether the lock at lockPath is stale: its holder
// process is no longer alive, or its contents are empty/unparseable. It also
// returns the trimmed contents it observed, so a follow-up removal can confirm
// nothing re-locked in the gap. Pure read — performs no mutation. Both the
// IsIndexing predicate and the tryRemoveStaleLock reap share this one liveness
// check; only the reap acts on the verdict.
func lockHeldByDeadProcess(lockPath string) (stale bool, observed string) {
	data, err := os.ReadFile(lockPath) // #nosec G304
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// TOCTOU: the lock was removed between the caller's os.Stat and
			// this read (e.g. the holder released it). There is no lock to
			// be stale — report stale so IsIndexing/reap treat it as gone,
			// not as an in-progress hold.
			return true, ""
		}
		// Some other read error (permissions, I/O) — we can't tell either
		// way; don't reap a lock we couldn't inspect.
		return false, ""
	}
	observed = strings.TrimSpace(string(data))
	if observed == "" {
		// Empty file (old format or crash mid-write) — treat as stale.
		return true, ""
	}
	pid, err := strconv.Atoi(observed)
	if err != nil {
		// Unparseable — treat as stale.
		return true, observed
	}

	// On Unix, os.FindProcess never fails for any int — liveness is decided
	// below by Signal(0). On Windows, FindProcess itself opens a process
	// handle (OpenProcess) and fails when the PID doesn't resolve to a live
	// process, so a lookup error there means the holder is dead.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true, observed
	}
	defer func() { _ = proc.Release() }() // release the Windows handle FindProcess opened; no-op on Unix

	sigErr := proc.Signal(syscall.Signal(0))
	switch {
	case sigErr == nil:
		// Holder alive.
		return false, observed
	case errors.Is(sigErr, os.ErrProcessDone) || errors.Is(sigErr, syscall.ESRCH):
		// Unix: no such process — holder is dead. Go's os package translates
		// the raw ESRCH errno from Kill into the os.ErrProcessDone sentinel
		// before returning it, so that's the one that actually matches in
		// practice; syscall.ESRCH is kept as a belt-and-suspenders check.
		return true, observed
	case errors.Is(sigErr, syscall.EPERM):
		// Unix: process exists but is owned by another user, so Signal(0)
		// is denied. It's alive — don't reap another user's live lock.
		return false, observed
	case runtime.GOOS == "windows":
		// Windows: Signal(0) isn't a supported signal type and always
		// errors here, live holder or not. FindProcess above already
		// confirmed the PID resolves to a real process, so treat as alive.
		return false, observed
	default:
		// Unknown error on a platform/signal combination we don't
		// specifically handle — err toward "alive" so we never reap a lock
		// we can't positively confirm is dead.
		return false, observed
	}
}

// tryRemoveStaleLock reaps the lock at lockPath when its holder is dead, removing
// it only if the file's contents still match what we verified as stale. Without
// the re-read-and-match, two parallel snipe runs starting against a stale lock
// could race: A reads the dead PID, C re-locks in the gap, then A unlinks C's
// live lock. The match check shrinks that window from "any past read" to a tight
// read→remove pair. This is the write-path reap; the read-path predicate
// (IsIndexing) deliberately does NOT call it.
func tryRemoveStaleLock(lockPath string) bool {
	stale, observed := lockHeldByDeadProcess(lockPath)
	if !stale {
		return false
	}
	return removeLockIfContentsMatch(lockPath, observed)
}

// removeLockIfContentsMatch unlinks lockPath only if its current contents (trimmed)
// still equal expect. If another process re-locked between the staleness verdict
// and now, the file's PID will have changed and we leave it alone.
func removeLockIfContentsMatch(lockPath, expect string) bool {
	cur, err := os.ReadFile(lockPath) // #nosec G304
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(cur)) != expect {
		return false
	}
	return os.Remove(lockPath) == nil
}

// ReleaseLock removes the lock file, but only if it still holds THIS process's
// PID. Release is ownership-checked for the same reason acquire is: after a
// stale-reap race, the lock we wrote may have been unlinked and re-created by
// another live writer. An unconditional os.Remove here would delete that
// foreign live lock, letting a third writer in and violating the single-writer
// invariant. Reusing removeLockIfContentsMatch keeps release symmetric with the
// ownership-aware acquire/stale-reap path. A no-op (foreign PID or already-gone
// lock) returns nil — releasing a lock we don't own is not an error.
func ReleaseLock(dbPath string) error {
	removeLockIfContentsMatch(LockPath(dbPath), strconv.Itoa(os.Getpid()))
	return nil
}

func verifyPragmaString(db *sql.DB, pragma, want string) error {
	var got string
	if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
		return fmt.Errorf("verify %s pragma: %w", pragma, err)
	}
	if got != want {
		return fmt.Errorf("verify %s pragma: got %q, want %q", pragma, got, want)
	}
	return nil
}

func verifyPragmaInt(db *sql.DB, pragma string, want int) error {
	var got int
	if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
		return fmt.Errorf("verify %s pragma: %w", pragma, err)
	}
	if got != want {
		return fmt.Errorf("verify %s pragma: got %d, want %d", pragma, got, want)
	}
	return nil
}
