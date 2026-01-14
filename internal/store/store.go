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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &Store{db: db, path: path}

	// Initialize schema
	if err := s.initSchema(); err != nil {
		db.Close()
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
