// Package notes is a fixture: a directory-of-markdown-files store.
package notes

import (
	"os"
	"path/filepath"
)

// SaveNote writes one note per id into a "notes" directory — a
// directory-of-documents store, the pattern DetectDataStores looks for.
func SaveNote(root, id string, body []byte) error {
	dir := filepath.Join(root, "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".md")
	return os.WriteFile(path, body, 0o644)
}
