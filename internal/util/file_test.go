package util_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/util"
)

func TestLoadFileLines_ReturnsExpectedLines_When_FileExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		contents   string
		want       []string
		wantErr    bool
		errMatcher func(error) bool
	}{
		{
			name:       "error: missing file returns not-exist error",
			wantErr:    true,
			errMatcher: os.IsNotExist,
		},
		{
			name:     "success: multi-line file returns split lines",
			contents: "first line\nsecond line\n",
			want:     []string{"first line", "second line"},
		},
		{
			name:     "boundary: empty file returns empty slice",
			contents: "",
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "sample.txt")
			if tc.wantErr {
				path = filepath.Join(dir, "missing.txt")
			} else {
				writeTempFile(t, path, tc.contents)
			}

			got, err := util.LoadFileLines(path)

			if tc.wantErr {
				require.Error(t, err)
				if tc.errMatcher != nil {
					assert.True(t, tc.errMatcher(err), "missing files should report not-exist")
				}
				return
			}
			require.NoError(t, err)

			if tc.want != nil {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("lines mismatch (-want +got):\n%s", diff)
				}
				assert.Len(t, got, len(tc.want), "line count should match the number of lines in the file")
			} else {
				assert.Empty(t, got, "empty files should not yield line content")
			}
		})
	}
}

func TestFileCache_EvictsOldestEntry_When_CapacityExceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		maxFiles int
	}{
		{
			name:     "boundary: max files set to one evicts prior entry",
			maxFiles: 1,
		},
		{
			name:     "success: max files set to two evicts least recently used entry",
			maxFiles: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fileA := filepath.Join(dir, "a.txt")
			fileB := filepath.Join(dir, "b.txt")
			fileC := filepath.Join(dir, "c.txt")
			writeTempFile(t, fileA, "alpha")
			writeTempFile(t, fileB, "bravo")
			writeTempFile(t, fileC, "charlie")

			cache := util.NewFileCache(tc.maxFiles)

			_, err := cache.LoadLines(fileA)
			require.NoError(t, err)
			_, err = cache.LoadLines(fileB)
			require.NoError(t, err)
			_, err = cache.LoadLines(fileC)
			require.NoError(t, err)

			assert.LessOrEqual(t, cache.Size(), tc.maxFiles, "cache should never exceed its max size")

			// Overwrite the oldest file to confirm eviction re-reads from disk.
			writeTempFile(t, fileA, "alpha-updated")
			got, err := cache.LoadLines(fileA)
			require.NoError(t, err)
			assert.Equal(t, "alpha-updated", got[0], "evicted files should be reloaded from disk")
		})
	}
}

func TestFileCache_RemovesEntries_When_ClearIsCalled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		maxFiles int
	}{
		{
			name:     "success: clear removes all cached entries",
			maxFiles: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			fileA := filepath.Join(dir, "a.txt")
			fileB := filepath.Join(dir, "b.txt")
			writeTempFile(t, fileA, "alpha")
			writeTempFile(t, fileB, "bravo")

			cache := util.NewFileCache(tc.maxFiles)
			_, err := cache.LoadLines(fileA)
			require.NoError(t, err)
			_, err = cache.LoadLines(fileB)
			require.NoError(t, err)

			cache.Clear()

			assert.Zero(t, cache.Size(), "clearing should drop all cached entries")
		})
	}
}

func writeTempFile(t *testing.T, path, contents string) {
	t.Helper()

	// Use explicit permissions so cache reads behave consistently across platforms.
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func TestWriteFileAtomic_CreatesFileWithExpectedContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	require.NoError(t, util.WriteFileAtomic(path, []byte("hello"), 0o600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(got))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteFileAtomic_OverwritesExistingFileAndLeavesNoTempfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

	require.NoError(t, util.WriteFileAtomic(path, []byte("new"), 0o600))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(entries), entries)
	}
}

func TestWriteFileAtomic_FailsCleanlyWhenDirMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "out.txt")
	err := util.WriteFileAtomic(path, []byte("x"), 0o600)
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}
