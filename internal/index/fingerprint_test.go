package index

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeFingerprint(t *testing.T) {
	// Create a temp directory with go.mod
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goMod, []byte("module test\n\ngo 1.21\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	fp, err := ComputeFingerprint(dir, "0.1.0")
	if err != nil {
		t.Fatalf("ComputeFingerprint failed: %v", err)
	}

	if fp.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", fp.Version, "0.1.0")
	}
	if fp.GoMod == "" {
		t.Error("GoMod hash should not be empty")
	}
	if fp.Combined == "" {
		t.Error("Combined hash should not be empty")
	}
}

func TestFingerprintDeterminism(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goMod, []byte("module test\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	fp1, _ := ComputeFingerprint(dir, "1.0.0")
	fp2, _ := ComputeFingerprint(dir, "1.0.0")

	if fp1.Combined != fp2.Combined {
		t.Error("Same inputs should produce same fingerprint")
	}
}

func TestFingerprintStableAcrossVersions(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(goMod, []byte("module test\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	fp1, _ := ComputeFingerprint(dir, "1.0.0")
	fp2, _ := ComputeFingerprint(dir, "2.0.0")

	if fp1.Combined != fp2.Combined {
		t.Error("Version change should not invalidate fingerprint")
	}
}

func TestFingerprintChangesWithGoMod(t *testing.T) {
	dir := t.TempDir()
	goMod := filepath.Join(dir, "go.mod")

	if err := os.WriteFile(goMod, []byte("module test\n"), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}
	fp1, _ := ComputeFingerprint(dir, "1.0.0")

	if err := os.WriteFile(goMod, []byte("module test\n\nrequire example.com/foo v1.0.0\n"), 0644); err != nil {
		t.Fatalf("Failed to update go.mod: %v", err)
	}
	fp2, _ := ComputeFingerprint(dir, "1.0.0")

	if fp1.Combined == fp2.Combined {
		t.Error("Different go.mod contents should produce different fingerprints")
	}
}

// An index fingerprinted before embedInputGen existed must not match the
// current one, or trySkipIndex keeps vectors built from the old embedding text.
func TestCombinedHashInvalidatesPreEmbedInputGenIndexes(t *testing.T) {
	fp := &Fingerprint{GoMod: "m", GoSum: "s", GoWork: "w", GoEnv: "e"}
	legacy := sha256.Sum256([]byte(strings.Join([]string{"m", "s", "w", "e"}, "|")))
	if got := computeCombinedHash(fp); got == hex.EncodeToString(legacy[:8]) {
		t.Errorf("combined hash %s equals the pre-embedInputGen hash; existing indexes would not rebuild", got)
	}
}

func TestFingerprintString(t *testing.T) {
	fp := &Fingerprint{Combined: "abc123"}
	if fp.String() != "abc123" {
		t.Errorf("String() = %q, want %q", fp.String(), "abc123")
	}
}
