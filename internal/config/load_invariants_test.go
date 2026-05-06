package config_test

import (
	"encoding/json"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/dkoosis/snipe/internal/config"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"os"
)

func TestLoad_ExpectedBehaviour_When_ConfigSourcesVary(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T) (projectRoot string)
		want    *config.Config
		wantErr error
		inspect func(*testing.T, *config.Config)
	}{
		{
			name: "error when home is a file and global path cannot be traversed",
			setup: func(t *testing.T) string {
				homeFile := filepath.Join(t.TempDir(), "home-file")
				require.NoError(t, os.WriteFile(homeFile, []byte("x"), 0o644))
				t.Setenv("HOME", homeFile)
				return t.TempDir()
			},
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "error when project root is a file and project config path cannot be traversed",
			setup: func(t *testing.T) string {
				t.Setenv("HOME", t.TempDir())
				projectRoot := filepath.Join(t.TempDir(), "not-a-dir")
				require.NoError(t, os.WriteFile(projectRoot, []byte("x"), 0o644))
				return projectRoot
			},
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "boundary empty project root loads defaults",
			setup: func(t *testing.T) string {
				t.Setenv("HOME", t.TempDir())
				return ""
			},
			want: &config.Config{Limit: 50, ContextLines: 3},
		},
		{
			name: "happy path merges defaults global and project with project precedence",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				globalDir := filepath.Join(home, ".config", "snipe")
				require.NoError(t, os.MkdirAll(globalDir, 0o755))
				writeJSON(t, filepath.Join(globalDir, "config.json"), &config.Config{Limit: 200, ContextLines: 9})

				projectRoot := t.TempDir()
				writeJSON(t, filepath.Join(projectRoot, ".snipe.json"), &config.Config{Limit: 120})
				return projectRoot
			},
			want: &config.Config{Limit: 120, ContextLines: 9},
		},
		{
			name: "happy path zero values in project do not erase non-zero upstream values",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				globalDir := filepath.Join(home, ".config", "snipe")
				require.NoError(t, os.MkdirAll(globalDir, 0o755))
				writeJSON(t, filepath.Join(globalDir, "config.json"), &config.Config{Limit: 111, ContextLines: 7})

				projectRoot := t.TempDir()
				writeJSON(t, filepath.Join(projectRoot, ".snipe.json"), &config.Config{Limit: 0, ContextLines: 0})
				return projectRoot
			},
			want: &config.Config{Limit: 111, ContextLines: 7},
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

			assertConfigInvariants(t, got)
			if tc.inspect != nil {
				tc.inspect(t, got)
			}
		})
	}
}

func writeJSON(t *testing.T, path string, cfg *config.Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func assertConfigInvariants(t *testing.T, cfg *config.Config) {
	t.Helper()
	require.NotNil(t, cfg)
	require.Greater(t, cfg.Limit, 0)
	require.Greater(t, cfg.ContextLines, 0)
}
