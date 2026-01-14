//go:build blackbox

package blackbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Test codebase: ../orca (read-only operations)
var testRepo = filepath.Join("..", "..", "..", "orca")

// snipeBin returns the path to the snipe binary
func snipeBin(t *testing.T) string {
	t.Helper()

	// Try bin/snipe first (from mage build)
	binPath := filepath.Join("..", "..", "bin", "snipe")
	if _, err := os.Stat(binPath); err == nil {
		abs, _ := filepath.Abs(binPath)
		return abs
	}

	// Try building it
	cmd := exec.Command("go", "build", "-o", "bin/snipe", ".")
	cmd.Dir = filepath.Join("..", "..")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build snipe: %v", err)
	}

	abs, _ := filepath.Abs(binPath)
	return abs
}

// runSnipe executes snipe with given args and returns stdout
func runSnipe(t *testing.T, args ...string) []byte {
	t.Helper()
	bin := snipeBin(t)

	cmd := exec.Command(bin, args...)
	cmd.Dir = testRepo

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("snipe %v failed: %v\nstderr: %s", args, err, exitErr.Stderr)
		}
		t.Fatalf("snipe %v failed: %v", args, err)
	}
	return output
}

// Response is the generic response structure
type Response struct {
	Results json.RawMessage `json:"results"`
	Meta    struct {
		Command    string `json:"command"`
		IndexState string `json:"index_state"`
		Ms         int64  `json:"ms"`
		Total      int    `json:"total"`
		Truncated  bool   `json:"truncated"`
	} `json:"meta"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestBlackbox_Version(t *testing.T) {
	bin := snipeBin(t)
	cmd := exec.Command(bin, "version")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("version failed: %v", err)
	}

	if len(output) == 0 {
		t.Error("version output should not be empty")
	}
}

func TestBlackbox_Search(t *testing.T) {
	output := runSnipe(t, "search", "--limit", "5", "func")

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Meta.Command != "search" {
		t.Errorf("Command = %q, want %q", resp.Meta.Command, "search")
	}
	if resp.Meta.Total == 0 {
		t.Error("Should find at least one 'func' match in orca")
	}
	if resp.Error != nil {
		t.Errorf("Unexpected error: %s", resp.Error.Message)
	}
}

func TestBlackbox_Index(t *testing.T) {
	// Create a temp directory for the index (don't pollute orca)
	tmpDir := t.TempDir()

	// Copy approach: index into a temp location by setting working dir
	// Actually, snipe index creates .snipe/ in the target dir
	// For read-only, we need to index to a different location
	// Let's skip this for now and test that index command works

	// For blackbox testing, we'll index snipe itself (not orca)
	snipeDir := filepath.Join("..", "..")
	absSnipe, _ := filepath.Abs(snipeDir)

	bin := snipeBin(t)
	cmd := exec.Command(bin, "index")
	cmd.Dir = absSnipe

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("index failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("index failed: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Meta.Command != "index" {
		t.Errorf("Command = %q, want %q", resp.Meta.Command, "index")
	}
	if resp.Meta.IndexState != "fresh" {
		t.Errorf("IndexState = %q, want %q", resp.Meta.IndexState, "fresh")
	}
	if resp.Meta.Total == 0 {
		t.Error("Should index at least one symbol")
	}

	// Cleanup: remove the .snipe directory
	_ = os.RemoveAll(filepath.Join(absSnipe, ".snipe"))
	_ = os.RemoveAll(tmpDir)
}

func TestBlackbox_DefByName(t *testing.T) {
	// First, ensure we have an index on snipe
	snipeDir := filepath.Join("..", "..")
	absSnipe, _ := filepath.Abs(snipeDir)

	bin := snipeBin(t)

	// Index first
	indexCmd := exec.Command(bin, "index")
	indexCmd.Dir = absSnipe
	if _, err := indexCmd.Output(); err != nil {
		t.Fatalf("index failed: %v", err)
	}
	defer os.RemoveAll(filepath.Join(absSnipe, ".snipe"))

	// Now test def
	cmd := exec.Command(bin, "def", "Symbol")
	cmd.Dir = absSnipe
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("def failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("def failed: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Meta.Command != "def" {
		t.Errorf("Command = %q, want %q", resp.Meta.Command, "def")
	}
	if resp.Error != nil {
		t.Errorf("Unexpected error: %s", resp.Error.Message)
	}
	if resp.Meta.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Meta.Total)
	}
}

func TestBlackbox_RefsByName(t *testing.T) {
	snipeDir := filepath.Join("..", "..")
	absSnipe, _ := filepath.Abs(snipeDir)

	bin := snipeBin(t)

	// Index first
	indexCmd := exec.Command(bin, "index")
	indexCmd.Dir = absSnipe
	if _, err := indexCmd.Output(); err != nil {
		t.Fatalf("index failed: %v", err)
	}
	defer os.RemoveAll(filepath.Join(absSnipe, ".snipe"))

	// Now test refs
	cmd := exec.Command(bin, "refs", "--limit", "10", "Symbol")
	cmd.Dir = absSnipe
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("refs failed: %v\nstderr: %s", err, exitErr.Stderr)
		}
		t.Fatalf("refs failed: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Meta.Command != "refs" {
		t.Errorf("Command = %q, want %q", resp.Meta.Command, "refs")
	}
	if resp.Error != nil {
		t.Errorf("Unexpected error: %s", resp.Error.Message)
	}
	if resp.Meta.Total == 0 {
		t.Error("Should find at least one reference to Symbol")
	}
}

func TestBlackbox_MissingIndex(t *testing.T) {
	// Test in a directory without an index
	tmpDir := t.TempDir()

	// Create a minimal go.mod
	goMod := filepath.Join(tmpDir, "go.mod")
	os.WriteFile(goMod, []byte("module test\n"), 0644)

	bin := snipeBin(t)

	cmd := exec.Command(bin, "def", "Foo")
	cmd.Dir = tmpDir
	output, _ := cmd.Output() // Ignore error, we expect one

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("Expected error for missing index")
	}
	if resp.Error.Code != "MISSING_INDEX" {
		t.Errorf("Error code = %q, want %q", resp.Error.Code, "MISSING_INDEX")
	}
}

func TestBlackbox_JSONFormat(t *testing.T) {
	output := runSnipe(t, "search", "--limit", "1", "package")

	var resp Response
	if err := json.Unmarshal(output, &resp); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify required fields exist
	if resp.Meta.Command == "" {
		t.Error("meta.command should not be empty")
	}
	if resp.Meta.Ms < 0 {
		t.Error("meta.ms should be non-negative")
	}
}
