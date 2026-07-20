//go:build blackbox

package blackbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var (
	binPath  string
	repoRoot string
)

func TestMain(m *testing.M) {
	code := runTests(m)
	os.Exit(code)
}

func runTests(m *testing.M) int {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repoRoot = root

	tmpDir, err := os.MkdirTemp("", "snipe-blackbox-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "snipe")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./")
	buildCmd.Dir = repoRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to build snipe:")
		fmt.Fprintln(os.Stderr, stdout.String())
		fmt.Fprintln(os.Stderr, stderr.String())
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return m.Run()
}

// run executes snipe with --format json (most tests parse JSON envelopes).
// Use runRaw for tests that need the default Claude-text output.
func run(t *testing.T, repoDir string, args ...string) (stdout []byte, stderr []byte, exitCode int) {
	t.Helper()
	// Prepend --format json unless the caller already specified --format.
	hasFormat := false
	for _, a := range args {
		if a == "--format" {
			hasFormat = true
			break
		}
	}
	if !hasFormat {
		args = append([]string{"--format", "json"}, args...)
	}
	return runRaw(t, repoDir, args...)
}

// runRaw executes snipe without injecting --format json.
func runRaw(t *testing.T, repoDir string, args ...string) (stdout []byte, stderr []byte, exitCode int) {
	t.Helper()

	cmd := exec.Command(binPath, args...)
	cmd.Dir = repoDir
	// KEYRING_DISABLE keeps the binary under test off the real macOS keychain,
	// so a developer's snipe/voyage keychain item can't defeat test isolation.
	cmd.Env = append(os.Environ(), "KEYRING_DISABLE=1")

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.Bytes()
	stderr = errBuf.Bytes()
	if err == nil {
		return stdout, stderr, 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, exitErr.ExitCode()
	}
	return stdout, stderr, 1
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find repo root from %s", wd)
}
