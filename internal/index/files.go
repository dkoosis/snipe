package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// FileInfo represents file metadata for change detection
type FileInfo struct {
	Path  string
	Mtime int64
	Hash  string
}

// ExtractFileInfo walks the repo directory to collect file hashes.
// Uses the same directory walk as DetectChanges to ensure the file sets
// match exactly — go/packages omits some files (build-tagged, integration
// tests) and includes cache paths, causing perpetual change detection.
func ExtractFileInfo(repoRoot string) ([]FileInfo, error) {
	var files []FileInfo
	err := walkGoFiles(repoRoot, DefaultExclude(), func(path string, _ os.DirEntry) error {
		info, fErr := computeFileInfo(path)
		if fErr != nil {
			return nil //nolint:nilerr // Skip unhashable files
		}
		files = append(files, info)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", repoRoot, err)
	}
	return files, nil
}

// walkGoFiles walks a directory tree visiting each .go file, skipping hidden
// directories and excluded directory names. Shared by WalkFileInfo and DetectChanges.
func walkGoFiles(dir string, exclude []string, fn func(path string, d os.DirEntry) error) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // Skip unreadable entries
		}
		if d.IsDir() {
			base := d.Name()
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			for _, pat := range exclude {
				if base == pat {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		return fn(path, d)
	})
}

// computeFileInfo computes the hash and mtime for a file
func computeFileInfo(path string) (FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat %s: %w", path, err)
	}

	hash, err := HashFileSHA256(path)
	if err != nil {
		return FileInfo{}, err // HashFileSHA256 already includes path context
	}

	return FileInfo{
		Path:  path,
		Mtime: stat.ModTime().Unix(),
		Hash:  hash,
	}, nil
}

// HashFileSHA256 computes a truncated SHA256 hash of a file (16 hex chars).
// Used during indexing and for staleness checks at query time.
func HashFileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path from go/packages load result
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}

	// Return first 8 bytes as hex (16 characters)
	return hex.EncodeToString(h.Sum(nil)[:8]), nil
}
