package store

import "fmt"

const schemaVersion = 5

// initSchema creates the database schema if it doesn't exist
func (s *Store) initSchema() error {
	schema := `
	-- Symbols table: function, type, interface, var, const definitions
	CREATE TABLE IF NOT EXISTS symbols (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		file_path TEXT NOT NULL,
		line_start INT NOT NULL,
		col_start INT NOT NULL,
		line_end INT NOT NULL,
		col_end INT NOT NULL,
		name_line INT NOT NULL DEFAULT 0,  -- Identifier position for call graph linkage
		name_col INT NOT NULL DEFAULT 0,
		signature TEXT,
		doc TEXT,
		receiver TEXT
	);

	-- References table: all identifier usages
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

	-- Call graph table: caller -> callee relationships
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

	-- Imports table: file -> package import relationships
	CREATE TABLE IF NOT EXISTS imports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_path TEXT NOT NULL,
		pkg_path TEXT NOT NULL,
		name TEXT,
		line INT NOT NULL,
		col INT NOT NULL,
		importer_pkg TEXT
	);

	-- Metadata table: version, timestamps, fingerprint
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT
	);

	-- File tracking for incremental updates
	CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		mtime INT NOT NULL,
		hash TEXT
	);

	-- Indexes for efficient queries
	CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
	CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_path);
	CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
	CREATE INDEX IF NOT EXISTS idx_refs_symbol ON refs(symbol_id);
	CREATE INDEX IF NOT EXISTS idx_refs_file ON refs(file_path);
	CREATE INDEX IF NOT EXISTS idx_callgraph_caller ON call_graph(caller_id);
	CREATE INDEX IF NOT EXISTS idx_callgraph_callee ON call_graph(callee_id);
	CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_path);
	CREATE INDEX IF NOT EXISTS idx_imports_pkg ON imports(pkg_path);

	-- Composite indexes for position queries (hot path for --at lookups)
	CREATE INDEX IF NOT EXISTS idx_symbols_position ON symbols(file_path, line_start, col_start);
	CREATE INDEX IF NOT EXISTS idx_refs_position ON refs(file_path, line, col);

	-- Composite indexes for common query patterns
	CREATE INDEX IF NOT EXISTS idx_refs_symbol_file ON refs(symbol_id, file_path, line);
	CREATE INDEX IF NOT EXISTS idx_symbols_file_kind ON symbols(file_path, kind);
	CREATE INDEX IF NOT EXISTS idx_symbols_name_kind ON symbols(name, kind);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	// Migrate existing databases: add name_line/name_col if missing
	// SQLite doesn't support ADD COLUMN IF NOT EXISTS, so we check first
	var colCount int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('symbols') WHERE name = 'name_line'`).Scan(&colCount)
	if err == nil && colCount == 0 {
		// Add columns to existing table
		s.db.Exec(`ALTER TABLE symbols ADD COLUMN name_line INT NOT NULL DEFAULT 0`)
		s.db.Exec(`ALTER TABLE symbols ADD COLUMN name_col INT NOT NULL DEFAULT 0`)
	}

	// Set schema version
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('schema_version', ?)`, schemaVersion); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return nil
}

// GetMeta retrieves a metadata value by key
func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetMeta sets a metadata value
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, key, value)
	return err
}
