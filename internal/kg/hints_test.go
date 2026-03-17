package kg_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/kg"
)

func TestGetHints_ReturnsExpectedResults_When_OrcaAvailabilityAndOutputVary(t *testing.T) {
	tests := []struct {
		name       string
		cfg        kg.Config
		orcaScript string
		want       []kg.Hint
	}{
		{
			name: "error: returns nil when orca is unavailable",
			cfg:  kg.Config{File: "internal/kg/hints.go"},
			want: nil,
		},
		{
			name:       "error: returns nil when orca command fails",
			cfg:        kg.Config{File: "internal/kg/hints.go"},
			orcaScript: "#!/bin/sh\nexit 1\n",
			want:       nil,
		},
		{
			name: "success: parses hints and extracts package suffix when file symbol and package are provided",
			cfg: kg.Config{
				File:    "internal/kg/hints.go",
				Symbol:  "GetHints",
				Package: "github.com/dkoosis/snipe/internal/kg",
			},
			orcaScript: `#!/bin/sh
query=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--query" ]; then
    shift
    query="$1"
    break
  fi
  shift
done

case "$query" in
  file:internal/kg/hints.go)
    echo "nug-file | trap:h | avoid stale index"
    ;;
  sym:GetHints)
    echo "not enough fields"
    echo "nug-symbol | pattern | query symbols first"
    ;;
  pkg:kg)
    echo "nug-pkg | map:l | package-level knowledge"
    ;;
esac
`,
			want: []kg.Hint{
				{ID: "nug-file", Kind: "trap", Severity: "h", Summary: "avoid stale index"},
				{ID: "nug-symbol", Kind: "pattern", Summary: "query symbols first"},
				{ID: "nug-pkg", Kind: "map", Severity: "l", Summary: "package-level knowledge"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.orcaScript != "" {
				binDir := t.TempDir()
				binaryName := "orca"
				if runtime.GOOS == "windows" {
					binaryName = "orca.bat"
					tc.orcaScript = strings.ReplaceAll(tc.orcaScript, "#!/bin/sh\n", "")
					tc.orcaScript = "@echo off\r\n" + strings.ReplaceAll(tc.orcaScript, "\n", "\r\n")
				}

				orcaPath := filepath.Join(binDir, binaryName)
				require.NoError(t, os.WriteFile(orcaPath, []byte(tc.orcaScript), 0o755))
				t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			} else {
				t.Setenv("PATH", t.TempDir())
			}

			got := kg.GetHints(tc.cfg)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("hints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
