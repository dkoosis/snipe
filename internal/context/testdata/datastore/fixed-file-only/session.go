// Package sessionish is a fixture: a directory holding a single fixed-name
// file — a state file, not a data store, and should NOT be detected.
package sessionish

import (
	"os"
	"path/filepath"
)

// Save writes one fixed-name file into a ".data" directory.
func Save(root string, data []byte) error {
	dir := filepath.Join(root, ".data")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, "session.json")
	return os.WriteFile(path, data, 0o600)
}
