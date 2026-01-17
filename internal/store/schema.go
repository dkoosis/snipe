package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const schemaVersion = 8

// migration represents a database migration.
type migration struct {
	version int
	name    string
	up      string
}

// migrations defines all database migrations in order.
// Each migration should be idempotent where possible.
var migrations = []migration{
	{
		version: 1,
		name:    "initial_schema",
		up: `
		CREATE TABLE IF NOT EXISTS symbols (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line_start INT NOT NULL,
			col_start INT NOT NULL,
			line_end INT NOT NULL,
			col_end INT NOT NULL,
			signature TEXT,
			doc TEXT,
			receiver TEXT
		);

		CREATE TABLE IF NOT EXISTS refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line INT NOT NULL,
			col INT NOT NULL,
			enclosing_id TEXT,
			snippet TEXT,
			FOREIGN KEY (symbol_id) REFERENCES symbols(id),
			FOREIGN KEY (enclosing_id) REFERENCES symbols(id)
		);

		CREATE TABLE IF NOT EXISTS call_graph (
			caller_id TEXT NOT NULL,
			callee_id TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line INT NOT NULL,
			col INT NOT NULL,
			PRIMARY KEY (caller_id, callee_id, line, col),
			FOREIGN KEY (caller_id) REFERENCES symbols(id),
			FOREIGN KEY (callee_id) REFERENCES symbols(id)
		);

		CREATE TABLE IF NOT EXISTS imports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			pkg_path TEXT NOT NULL,
			name TEXT,
			line INT NOT NULL,
			col INT NOT NULL,
			importer_pkg TEXT
		);

		CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			mtime INT NOT NULL,
			hash TEXT
		);
		`,
	},
	{
		version: 2,
		name:    "basic_indexes",
		up: `
		CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
		CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_path);
		CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
		CREATE INDEX IF NOT EXISTS idx_refs_symbol ON refs(symbol_id);
		CREATE INDEX IF NOT EXISTS idx_refs_file ON refs(file_path);
		CREATE INDEX IF NOT EXISTS idx_callgraph_caller ON call_graph(caller_id);
		CREATE INDEX IF NOT EXISTS idx_callgraph_callee ON call_graph(callee_id);
		CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_path);
		CREATE INDEX IF NOT EXISTS idx_imports_pkg ON imports(pkg_path);
		`,
	},
	{
		version: 3,
		name:    "composite_indexes",
		up: `
		CREATE INDEX IF NOT EXISTS idx_symbols_position ON symbols(file_path, line_start, col_start);
		CREATE INDEX IF NOT EXISTS idx_refs_position ON refs(file_path, line, col);
		CREATE INDEX IF NOT EXISTS idx_refs_symbol_file ON refs(symbol_id, file_path, line);
		CREATE INDEX IF NOT EXISTS idx_symbols_file_kind ON symbols(file_path, kind);
		CREATE INDEX IF NOT EXISTS idx_symbols_name_kind ON symbols(name, kind);
		`,
	},
	{
		version: 4,
		name:    "add_name_position_columns",
		up:      ``, // Handled specially below due to SQLite limitations
	},
	{
		version: 5,
		name:    "migrations_table",
		up: `
		CREATE TABLE IF NOT EXISTS migrations (
			version INT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		);
		`,
	},
	{
		version: 6,
		name:    "no_op",
		up:      ``, // No-op placeholder
	},
	{
		version: 7,
		name:    "embeddings_table",
		up: `
		CREATE TABLE IF NOT EXISTS embeddings (
			symbol_id TEXT PRIMARY KEY,
			embedding BLOB NOT NULL,
			model TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY (symbol_id) REFERENCES symbols(id)
		);
		CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings(model);
		`,
	},
	{
		version: 8,
		name:    "add_file_path_rel",
		up:      ``, // Handled specially - need to add columns and index
	},
}

// initSchema creates and migrates the database schema.
func (s *Store) initSchema() error {
	// Ensure migrations table exists first (bootstrap)
	if _, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Get current migration version
	currentVersion := s.getCurrentMigrationVersion()

	// Run pending migrations
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		if err := s.runMigration(m); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.version, m.name, err)
		}
	}

	// Set schema version in meta table
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)`, schemaVersion); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return nil
}

// getCurrentMigrationVersion returns the highest applied migration version.
func (s *Store) getCurrentMigrationVersion() int {
	var version int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM migrations`).Scan(&version)
	if err != nil {
		return 0
	}
	return version
}

// runMigration executes a single migration in a transaction.
func (s *Store) runMigration(m migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			// Log rollback error in production
			_ = err
		}
	}()

	// Handle special migrations
	switch m.version {
	case 4:
		// SQLite doesn't support ADD COLUMN IF NOT EXISTS
		// Check if columns exist before adding
		if err := s.addNamePositionColumns(tx); err != nil {
			return err
		}
	case 8:
		// Add file_path_rel columns for efficient exact-match queries
		if err := s.addFilePathRelColumns(tx); err != nil {
			return err
		}
	default:
		// Run standard migration SQL
		if m.up != "" {
			if _, err := tx.Exec(m.up); err != nil {
				return fmt.Errorf("execute migration SQL: %w", err)
			}
		}
	}

	// Record migration
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// addNamePositionColumns handles migration 4 - adding name_line/name_col columns.
func (s *Store) addNamePositionColumns(tx *sql.Tx) error {
	// Check if name_line column exists
	var colCount int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('symbols') WHERE name = 'name_line'`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("check column existence: %w", err)
	}

	if colCount == 0 {
		// Add columns to existing table
		if _, err := tx.Exec(`ALTER TABLE symbols ADD COLUMN name_line INT NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add name_line column: %w", err)
		}
		if _, err := tx.Exec(`ALTER TABLE symbols ADD COLUMN name_col INT NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add name_col column: %w", err)
		}
	}

	return nil
}

// addFilePathRelColumns handles migration 8 - adding file_path_rel column.
// This stores repo-relative paths for efficient exact-match queries.
func (s *Store) addFilePathRelColumns(tx *sql.Tx) error {
	// Check if file_path_rel column exists on symbols
	var colCount int
	err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('symbols') WHERE name = 'file_path_rel'`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("check column existence: %w", err)
	}

	if colCount == 0 {
		// Add column to symbols table (nullable initially for backfill compatibility)
		if _, err := tx.Exec(`ALTER TABLE symbols ADD COLUMN file_path_rel TEXT`); err != nil {
			return fmt.Errorf("add symbols.file_path_rel column: %w", err)
		}

		// Add index for efficient lookups
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_symbols_file_path_rel ON symbols(file_path_rel)`); err != nil {
			return fmt.Errorf("add idx_symbols_file_path_rel index: %w", err)
		}
	}

	// Check if file_path_rel column exists on refs
	err = tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('refs') WHERE name = 'file_path_rel'`).Scan(&colCount)
	if err != nil {
		return fmt.Errorf("check refs column existence: %w", err)
	}

	if colCount == 0 {
		// Add column to refs table
		if _, err := tx.Exec(`ALTER TABLE refs ADD COLUMN file_path_rel TEXT`); err != nil {
			return fmt.Errorf("add refs.file_path_rel column: %w", err)
		}

		// Add index for efficient lookups
		if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_refs_file_path_rel ON refs(file_path_rel)`); err != nil {
			return fmt.Errorf("add idx_refs_file_path_rel index: %w", err)
		}
	}

	return nil
}

// GetMeta retrieves a metadata value by key.
func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetMeta sets a metadata value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, key, value)
	return err
}

// GetMigrationHistory returns all applied migrations.
func (s *Store) GetMigrationHistory() ([]struct {
	Version   int
	Name      string
	AppliedAt string
}, error) {
	rows, err := s.db.Query(`SELECT version, name, applied_at FROM migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []struct {
		Version   int
		Name      string
		AppliedAt string
	}

	for rows.Next() {
		var h struct {
			Version   int
			Name      string
			AppliedAt string
		}
		if err := rows.Scan(&h.Version, &h.Name, &h.AppliedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}

	return history, rows.Err()
}
