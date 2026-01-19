package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Balance durability and latency for WAL workloads
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("set synchronous: %w", err)
	}

	// Keep temporary data in memory for speed
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("set temp_store: %w", err)
	}

	// Set busy timeout to avoid "database is locked" errors during concurrent access
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	// Limit connections to avoid lock contention
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db, path: path}

	// Initialize schema
	if err := s.initSchema(); err != nil {
		_ = db.Close() // G104: cleanup on error path
		return nil, fmt.Errorf("initialize schema: %w", err)
	}

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

// IsIndexing checks if indexing is in progress (lock file exists)
func IsIndexing(dbPath string) bool {
	_, err := os.Stat(LockPath(dbPath))
	return err == nil
}

// AcquireLock creates a lock file for indexing
func AcquireLock(dbPath string) error {
	lockPath := LockPath(dbPath)
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(lockPath), 0750); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	f, err := os.Create(lockPath) // #nosec G304 -- lockPath derived from dbPath (index lock)
	if err != nil {
		return fmt.Errorf("create lock file: %w", err)
	}
	return f.Close()
}

// ReleaseLock removes the lock file
func ReleaseLock(dbPath string) error {
	return os.Remove(LockPath(dbPath))
}
