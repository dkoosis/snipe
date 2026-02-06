//go:build eval

package eval

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var (
	binPath  string
	repoRoot string
)

// RepoConfig describes how to locate a benchmark repo.
type RepoConfig struct {
	EnvVar  string // environment variable override (e.g., ORCA_DIR)
	Default string // default relative path from snipe root
	Repo    string // git clone URL for public repos
}

var repos = map[string]RepoConfig{
	"orca":  {EnvVar: "ORCA_DIR", Default: "../orca", Repo: ""},
	"chi":   {Default: "../chi", Repo: "https://github.com/go-chi/chi"},
	"cobra": {Default: "../cobra", Repo: "https://github.com/spf13/cobra"},
	"bbolt": {Default: "../bbolt", Repo: "https://github.com/etcd-io/bbolt"},
	"fzf":   {Default: "../fzf", Repo: "https://github.com/junegunn/fzf"},
}

func TestMain(m *testing.M) {
	code := runSetup(m)
	os.Exit(code)
}

func runSetup(m *testing.M) int {
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	repoRoot = root

	tmpDir, err := os.MkdirTemp("", "snipe-eval-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	binPath = filepath.Join(tmpDir, "snipe")
	buildCmd := exec.Command("go", "build", "-o", binPath, "./")
	buildCmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to build snipe:")
		fmt.Fprintln(os.Stderr, stderr.String())
		return 1
	}

	return m.Run()
}

// run executes a snipe command in the given repo directory.
func run(t *testing.T, repoDir string, args ...string) (stdout, stderr []byte, exitCode int) {
	t.Helper()

	cmd := exec.Command(binPath, args...)
	cmd.Dir = repoDir
	cmd.Env = os.Environ()

	var outBuf, errBuf bytes.Buffer
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

// resolveRepoDir returns the directory for a benchmark repo, or "" if unavailable.
func resolveRepoDir(name string) string {
	cfg, ok := repos[name]
	if !ok {
		return ""
	}

	// Check environment variable override
	if cfg.EnvVar != "" {
		if dir := os.Getenv(cfg.EnvVar); dir != "" {
			if hasIndex(dir) {
				return dir
			}
		}
	}

	// Check default relative path
	if cfg.Default != "" {
		dir := filepath.Join(repoRoot, cfg.Default)
		if hasIndex(dir) {
			return dir
		}
	}

	// Check .eval-repos/<name>
	dir := filepath.Join(repoRoot, ".eval-repos", name)
	if hasIndex(dir) {
		return dir
	}

	return ""
}

// hasIndex checks if a directory exists and has a snipe index.
func hasIndex(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".snipe", "index.db"))
	return err == nil
}

// checkIndexFreshness warns if the index is older than the newest .go file.
func checkIndexFreshness(t *testing.T, dir string) {
	t.Helper()
	indexPath := filepath.Join(dir, ".snipe", "index.db")
	indexInfo, err := os.Stat(indexPath)
	if err != nil {
		return // no index to check
	}
	indexMod := indexInfo.ModTime()

	var newestGo time.Time
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "vendor" || base == "node_modules" || base == ".snipe" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" && info.ModTime().After(newestGo) {
			newestGo = info.ModTime()
		}
		return nil
	})

	if !newestGo.IsZero() && newestGo.After(indexMod) {
		t.Logf("WARNING: index for %s is stale (index: %s, newest .go: %s)",
			filepath.Base(dir),
			indexMod.Format("2006-01-02 15:04"),
			newestGo.Format("2006-01-02 15:04"))
	}
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
