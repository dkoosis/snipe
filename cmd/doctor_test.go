package cmd

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dkoosis/snipe/internal/store"
)

func TestCheckRipgrep_Available(t *testing.T) {
	// Skip if rg is not installed
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed, skipping positive test")
	}

	check := checkRipgrep()

	if !check.OK {
		t.Errorf("checkRipgrep().OK = false, want true when rg is installed")
	}
	if check.Name != "ripgrep" {
		t.Errorf("check.Name = %q, want 'ripgrep'", check.Name)
	}
	if check.Message == "" {
		t.Error("check.Message should not be empty")
	}
}

func TestCheckRipgrep_MissingRg(t *testing.T) {
	// Save original PATH
	origPath := os.Getenv("PATH")
	defer os.Setenv("PATH", origPath)

	// Set PATH to empty to simulate missing rg
	os.Setenv("PATH", "")

	check := checkRipgrep()

	if check.OK {
		t.Error("checkRipgrep().OK = true, want false when rg is not in PATH")
	}
	if check.Code != DoctorRGMissing {
		t.Errorf("check.Code = %q, want %q", check.Code, DoctorRGMissing)
	}
	if check.Remediation == "" {
		t.Error("check.Remediation should not be empty")
	}
}

func TestCheckIndex_NotInGitRepo(t *testing.T) {
	// Use temp dir which is not a git repo
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	check := checkIndex()

	if check.OK {
		t.Error("checkIndex().OK = true, want false when not in git repo")
	}
	if check.Name != "index" {
		t.Errorf("check.Name = %q, want 'index'", check.Name)
	}
}

func TestDoctorCheck_Fields(t *testing.T) {
	check := DoctorCheck{
		Name:        "test",
		OK:          false,
		Code:        DoctorIndexCorrupt,
		Message:     "test failed",
		Remediation: remediationReindex,
		Details:     "details here",
	}

	if check.Code != "INDEX_CORRUPT" {
		t.Errorf("check.Code = %q, want %q", check.Code, "INDEX_CORRUPT")
	}
	if check.Remediation != remediationReindex {
		t.Errorf("check.Remediation = %q, want %q", check.Remediation, remediationReindex)
	}
}

func TestCheckIndex_ValidDB(t *testing.T) {
	// Create a temp dir with .git and a valid snipe index
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()
	// Create .git marker
	if err := os.Mkdir(tmpDir+"/.git", 0750); err != nil {
		t.Fatal(err)
	}

	// Create valid index
	snipeDir := tmpDir + "/.snipe"
	if err := os.MkdirAll(snipeDir, 0750); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(tmpDir + "/.snipe/index.db")
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	s.Close()

	os.Chdir(tmpDir)
	check := checkIndex()

	if !check.OK {
		t.Errorf("checkIndex().OK = false for valid DB, message: %s, details: %s", check.Message, check.Details)
	}
}

func TestCheckIndex_CorruptDB(t *testing.T) {
	// Create a temp dir with .git and a corrupt index
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)

	tmpDir := t.TempDir()
	if err := os.Mkdir(tmpDir+"/.git", 0750); err != nil {
		t.Fatal(err)
	}

	snipeDir := tmpDir + "/.snipe"
	if err := os.MkdirAll(snipeDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Write garbage as index file
	if err := os.WriteFile(tmpDir+"/.snipe/index.db", []byte("not a sqlite database"), 0600); err != nil {
		t.Fatal(err)
	}

	os.Chdir(tmpDir)
	check := checkIndex()

	if check.OK {
		t.Error("checkIndex().OK = true for corrupt DB, want false")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "30 seconds"},
		{"minutes", 5 * time.Minute, "5 minutes"},
		{"hours", 2 * time.Hour, "2.0 hours"},
		{"days", 48 * time.Hour, "2.0 days"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
