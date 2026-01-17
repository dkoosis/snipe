package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
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
	if check.Details == "" {
		t.Error("check.Details should contain install instructions")
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

func TestDoctorResult_JSON(t *testing.T) {
	result := &DoctorResult{
		OK: false,
		Checks: []DoctorCheck{
			{
				Name:    "test",
				OK:      false,
				Message: "test failed",
				Details: "details here",
			},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded DoctorResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.OK != false {
		t.Errorf("decoded.OK = %v, want false", decoded.OK)
	}
	if len(decoded.Checks) != 1 {
		t.Fatalf("len(decoded.Checks) = %d, want 1", len(decoded.Checks))
	}
	if decoded.Checks[0].Name != "test" {
		t.Errorf("decoded.Checks[0].Name = %q, want 'test'", decoded.Checks[0].Name)
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
