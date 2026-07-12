package query

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMergeRanges(t *testing.T) {
	tests := []struct {
		name string
		in   []LineRange
		want []LineRange
	}{
		{"empty", nil, nil},
		{"single", []LineRange{{5, 10}}, []LineRange{{5, 10}}},
		{"inverted bounds swapped", []LineRange{{10, 5}}, []LineRange{{5, 10}}},
		{"overlapping merged", []LineRange{{1, 5}, {4, 8}}, []LineRange{{1, 8}}},
		{"adjacent merged", []LineRange{{1, 3}, {4, 6}}, []LineRange{{1, 6}}},
		{"disjoint kept", []LineRange{{1, 3}, {10, 12}}, []LineRange{{1, 3}, {10, 12}}},
		{"unsorted input", []LineRange{{10, 12}, {1, 3}, {2, 4}}, []LineRange{{1, 4}, {10, 12}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeRanges(tt.in))
		})
	}
}

// insertOverlapSym inserts a symbol with an explicit line range, for overlap
// testing where the fixed 1-10 range from insertTestSym isn't precise enough.
func insertOverlapSym(t *testing.T, db *sql.DB, id, name, filePath, filePathRel string, lineStart, lineEnd int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path,
		line_start, col_start, line_end, col_end, signature, doc, receiver)
		VALUES (?, ?, 'func', ?, ?, 'pkg/example', ?, 1, ?, 1, '', '', '')`,
		id, name, filePath, filePathRel, lineStart, lineEnd)
	require.NoError(t, err)
}

// markFresh writes a real file to disk and records it in the files table
// with a stored mtime at or after the file's actual mtime, so
// CheckPathStaleness sees it as fresh (hunk-precise matching applies).
func markFresh(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("package example\n"), 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	// Store a stored mtime comfortably ahead of the actual mtime so the
	// comparison (`disk mtime > stored mtime`) is unambiguously false,
	// regardless of filesystem timestamp resolution.
	storedMtime := info.ModTime().Add(time.Hour).Unix()
	_, err = db.Exec(`INSERT INTO files (path, mtime, hash) VALUES (?, ?, '')`, path, storedMtime)
	require.NoError(t, err)
}

// markStaleByMtime writes a real file to disk and records a stored mtime far
// in the past, so CheckPathStaleness reports it stale via the mtime-changed
// path (as opposed to the "not indexed at all" path).
func markStaleByMtime(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("package example\n"), 0o600))
	_, err := db.Exec(`INSERT INTO files (path, mtime, hash) VALUES (?, 0, '')`, path)
	require.NoError(t, err)
}

func TestFindOverlappingSymbols_ReturnsEmpty_When_NoChanges(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	got, err := FindOverlappingSymbols(db, "", nil)
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = FindOverlappingSymbols(db, "", map[string][]LineRange{})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestFindOverlappingSymbols_ReturnsSymbol_When_HunkInsideSymbol(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "order.go")
	markFresh(t, db, path)

	insertOverlapSym(t, db, "s1", "ProcessOrder", path, "order.go", 10, 50)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {{Start: 20, End: 25}}, // hunk entirely inside the symbol's body
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ProcessOrder", got[0].Name)
}

func TestFindOverlappingSymbols_ReturnsSymbol_When_SymbolInsideHunk(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "small.go")
	markFresh(t, db, path)

	insertOverlapSym(t, db, "s1", "Tiny", path, "small.go", 10, 15)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {{Start: 1, End: 100}}, // hunk spans well beyond the symbol
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Tiny", got[0].Name)
}

func TestFindOverlappingSymbols_ReturnsSymbol_When_BoundaryLinesTouch(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "boundary.go")
	markFresh(t, db, path)

	// Symbol occupies lines 10-20. A hunk ending exactly at the symbol's
	// first line, and a hunk starting exactly at the symbol's last line,
	// both count as overlaps — boundaries are inclusive.
	insertOverlapSym(t, db, "s1", "AtStart", path, "boundary.go", 10, 20)
	insertOverlapSym(t, db, "s2", "AtEnd", path, "boundary.go", 30, 40)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {
			{Start: 1, End: 10},  // touches AtStart's first line only
			{Start: 40, End: 45}, // touches AtEnd's last line only
		},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	require.ElementsMatch(t, []string{"AtStart", "AtEnd"}, names)
}

func TestFindOverlappingSymbols_ExcludesSymbol_When_RangeDoesNotOverlap(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "far.go")
	markFresh(t, db, path)

	insertOverlapSym(t, db, "s1", "Untouched", path, "far.go", 100, 120)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {{Start: 1, End: 99}}, // one line short of the symbol's start
	})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestFindOverlappingSymbols_ReturnsAllFileSymbols_When_FileNotIndexed(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()

	// No row in the files table at all — CheckPathStaleness treats an
	// unindexed file as stale without touching disk.
	path := "/repo/unindexed.go"
	insertOverlapSym(t, db, "s1", "InRange", path, "unindexed.go", 10, 20)
	insertOverlapSym(t, db, "s2", "FarOutOfRange", path, "unindexed.go", 500, 520)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {{Start: 10, End: 12}}, // would only hit "InRange" under hunk-precise matching
	})
	require.NoError(t, err)
	// Staleness fallback: both symbols come back, not just the one the hunk touches.
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	require.ElementsMatch(t, []string{"InRange", "FarOutOfRange"}, names)
}

func TestFindOverlappingSymbols_ReturnsAllFileSymbols_When_MtimeIsStale(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "stale.go")
	markStaleByMtime(t, db, path)

	insertOverlapSym(t, db, "s1", "InRange", path, "stale.go", 10, 20)
	insertOverlapSym(t, db, "s2", "FarOutOfRange", path, "stale.go", 500, 520)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {{Start: 10, End: 12}},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestFindOverlappingSymbols_BatchesAcrossFiles_When_MultipleFilesChanged(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	freshPath := filepath.Join(dir, "fresh.go")
	markFresh(t, db, freshPath)
	stalePath := "/repo/stale-unindexed.go"

	insertOverlapSym(t, db, "s1", "FreshHit", freshPath, "fresh.go", 10, 20)
	insertOverlapSym(t, db, "s2", "FreshMiss", freshPath, "fresh.go", 500, 520)
	insertOverlapSym(t, db, "s3", "StaleAlwaysIncluded", stalePath, "stale-unindexed.go", 500, 520)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		freshPath: {{Start: 10, End: 15}},
		stalePath: {{Start: 1, End: 2}}, // irrelevant range — stale fallback ignores it
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	require.ElementsMatch(t, []string{"FreshHit", "StaleAlwaysIncluded"}, names)
}

func TestFindOverlappingSymbols_HandlesMultipleHunksInOneFile_When_RangesDisjoint(t *testing.T) {
	db := setupTestsDB(t)
	defer db.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.go")
	markFresh(t, db, path)

	insertOverlapSym(t, db, "s1", "First", path, "multi.go", 10, 20)
	insertOverlapSym(t, db, "s2", "Second", path, "multi.go", 100, 120)
	insertOverlapSym(t, db, "s3", "Neither", path, "multi.go", 200, 220)

	got, err := FindOverlappingSymbols(db, "", map[string][]LineRange{
		path: {
			{Start: 15, End: 15},
			{Start: 110, End: 110},
		},
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	names := []string{got[0].Name, got[1].Name}
	require.ElementsMatch(t, []string{"First", "Second"}, names)
}
