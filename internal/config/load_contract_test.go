package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/config"
)

func TestLoad_MergesConfiguration_When_SourcesPresent(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T) string
		want    *config.Config
		wantErr error
		inspect func(*testing.T, *config.Config)
	}{
		{
			name: "error: project root as file surfaces not-a-directory error",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				projectRoot := filepath.Join(t.TempDir(), "project-root-file")
				require.NoError(t, os.WriteFile(projectRoot, []byte("x"), 0o644))
				return projectRoot
			},
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "error: home as file surfaces not-a-directory error",
			setup: func(t *testing.T) string {
				homeFile := filepath.Join(t.TempDir(), "home-file")
				require.NoError(t, os.WriteFile(homeFile, []byte("x"), 0o644))
				t.Setenv("HOME", homeFile)
				return t.TempDir()
			},
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "boundary: zero-valued project fields do not override defaults",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				project := t.TempDir()
				writeConfig(t, filepath.Join(project, ".snipe.json"), &config.Config{Limit: 0, ContextLines: 0})
				return project
			},
			want: &config.Config{Limit: 50, ContextLines: 3},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.Positive(t, got.Limit)
				require.Positive(t, got.ContextLines)
			},
		},
		{
			name: "happy path: project overrides global while preserving unspecified global value",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				globalDir := filepath.Join(home, ".config", "snipe")
				require.NoError(t, os.MkdirAll(globalDir, 0o755))
				writeConfig(t, filepath.Join(globalDir, "config.json"), &config.Config{Limit: 200, ContextLines: 10})

				project := t.TempDir()
				writeConfig(t, filepath.Join(project, ".snipe.json"), &config.Config{Limit: 75})
				return project
			},
			want: &config.Config{Limit: 75, ContextLines: 10},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.GreaterOrEqual(t, got.Limit, 1)
				require.GreaterOrEqual(t, got.ContextLines, 1)
			},
		},
		{
			name: "happy path: missing config files returns defaults",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				return t.TempDir()
			},
			want: &config.Config{Limit: 50, ContextLines: 3},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.NotNil(t, got)
				require.Equal(t, true, got.Limit > 0 && got.ContextLines > 0)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := tc.setup(t)
			got, err := config.Load(projectRoot)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("diff (-want +got):\n%s", diff)
			}

			if tc.inspect != nil {
				tc.inspect(t, got)
			}
		})
	}
}

func writeConfig(t *testing.T, path string, cfg *config.Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}
