package config_test

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/config"
)

func TestLoad_ExpectedBehaviour_When_ConfigSourcesChange(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*testing.T) string
		want    *config.Config
		wantErr error
		inspect func(*testing.T, *config.Config)
	}{
		{
			name: "error: malformed project path as file returns not-a-directory",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				projectRootFile := filepath.Join(t.TempDir(), "project-root")
				require.NoError(t, os.WriteFile(projectRootFile, []byte("x"), 0o644))
				return projectRootFile
			},
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "error: malformed global config surfaces parse failure",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				globalDir := filepath.Join(home, ".config", "snipe")
				require.NoError(t, os.MkdirAll(globalDir, 0o755))
				cfgPath := filepath.Join(globalDir, "config.json")
				require.NoError(t, os.WriteFile(cfgPath, []byte(`{"limit":`), 0o644))
				return t.TempDir()
			},
			wantErr: io.ErrUnexpectedEOF,
		},
		{
			name: "boundary: empty project root uses defaults when no global config",
			setup: func(t *testing.T) string {
				t.Setenv("HOME", t.TempDir())
				return ""
			},
			want: &config.Config{Limit: 50, ContextLines: 3},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.Greater(t, got.Limit, 0)
				require.Greater(t, got.ContextLines, 0)
			},
		},
		{
			name: "boundary: zero project values cannot erase defaults",
			setup: func(t *testing.T) string {
				t.Setenv("HOME", t.TempDir())
				project := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(project, ".snipe.json"), []byte(`{"limit":0,"context_lines":0}`), 0o644))
				return project
			},
			want: &config.Config{Limit: 50, ContextLines: 3},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.Greater(t, got.Limit, 0)
				require.Greater(t, got.ContextLines, 0)
			},
		},
		{
			name: "happy path: project overrides global while keeping other global settings",
			setup: func(t *testing.T) string {
				home := t.TempDir()
				t.Setenv("HOME", home)
				globalDir := filepath.Join(home, ".config", "snipe")
				require.NoError(t, os.MkdirAll(globalDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(`{"limit":200,"context_lines":9}`), 0o644))
				project := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(project, ".snipe.json"), []byte(`{"limit":75}`), 0o644))
				return project
			},
			want: &config.Config{Limit: 75, ContextLines: 9},
			inspect: func(t *testing.T, got *config.Config) {
				t.Helper()
				require.Greater(t, got.Limit, 0)
				require.Greater(t, got.ContextLines, 0)
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
