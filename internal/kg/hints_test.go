package kg_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

			// GetHints applies a 2s per-query timeout (up to 3 queries in the
			// success case). Under `go test ./...` with GOMAXPROCS saturated by
			// concurrent packages, the shim subprocess can be scheduled late
			// enough to blow that budget even though it does real work in
			// milliseconds. Retry with a generous overall ceiling instead of
			// asserting on the first attempt, so the result is load-independent
			// rather than timing-sensitive.
			const (
				retryBudget = 10 * time.Second
				retryPoll   = 100 * time.Millisecond
			)
			var got []kg.Hint
			deadline := time.Now().Add(retryBudget)
			for {
				got = kg.GetHints(context.Background(), tc.cfg)
				if cmp.Diff(tc.want, got) == "" || time.Now().After(deadline) {
					break
				}
				time.Sleep(retryPoll)
			}

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("hints mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGetHints_HonorsContextCancel asserts that a parent ctx cancel aborts
// in-flight orca subprocesses within bounded time, instead of wedging snipe
// for the duration of a hung child.
func TestGetHints_HonorsContextCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hung-orca shim relies on POSIX sleep")
	}

	binDir := t.TempDir()
	// orca shim that hangs forever — exercises the ctx-cancel path.
	orcaPath := filepath.Join(binDir, "orca")
	require.NoError(t, os.WriteFile(orcaPath, []byte("#!/bin/sh\nsleep 60\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	got := kg.GetHints(ctx, kg.Config{File: "x.go", Symbol: "Y", Package: "z"})
	elapsed := time.Since(start)

	if got != nil {
		t.Errorf("expected nil hints from cancelled call, got %#v", got)
	}
	// Per-query timeout is 2s; 3 queries × 2s = 6s worst case without ctx wiring.
	// Cancel must short-circuit well under that. Allow generous slack for CI.
	if elapsed > 3*time.Second {
		t.Errorf("GetHints did not honor ctx cancel; took %v", elapsed)
	}
}
