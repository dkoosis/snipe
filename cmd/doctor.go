package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/store"
)

// DoctorResult represents the result of the doctor check.
type DoctorResult struct {
	OK     bool          `json:"ok"`
	Checks []DoctorCheck `json:"checks"`
}

// DoctorCheck represents a single diagnostic check.
type DoctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Details string `json:"details,omitempty"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check snipe installation and configuration",
	Long: `Runs diagnostic checks to verify snipe is properly installed and configured.

Checks include:
- ripgrep (rg) availability and version
- Index database existence and freshness`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	result := &DoctorResult{
		OK:     true,
		Checks: []DoctorCheck{},
	}

	// Check ripgrep
	rgCheck := checkRipgrep()
	result.Checks = append(result.Checks, rgCheck)
	if !rgCheck.OK {
		result.OK = false
	}

	// Check index
	indexCheck := checkIndex()
	result.Checks = append(result.Checks, indexCheck)
	if !indexCheck.OK {
		result.OK = false
	}

	// Output as JSON
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func checkRipgrep() DoctorCheck {
	check := DoctorCheck{
		Name: "ripgrep",
	}

	path, err := exec.LookPath("rg")
	if err != nil {
		check.OK = false
		check.Message = "ripgrep (rg) not found"
		check.Details = "Install from https://github.com/BurntSushi/ripgrep\n" +
			"  macOS: brew install ripgrep\n" +
			"  Ubuntu/Debian: apt install ripgrep\n" +
			"  Windows: choco install ripgrep"
		return check
	}

	// Get version
	out, err := exec.Command("rg", "--version").Output()
	if err != nil {
		check.OK = true
		check.Message = fmt.Sprintf("ripgrep found at %s (version unknown)", path)
		return check
	}

	version := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	check.OK = true
	check.Message = version
	check.Details = fmt.Sprintf("Path: %s", path)

	return check
}

func checkIndex() DoctorCheck {
	check := DoctorCheck{
		Name: "index",
	}

	// Find project root (look for .git directory)
	cwd, err := os.Getwd()
	if err != nil {
		check.OK = false
		check.Message = "could not determine working directory"
		return check
	}

	projectRoot := findProjectRoot(cwd)
	if projectRoot == "" {
		check.OK = false
		check.Message = "not in a git repository"
		check.Details = "Run 'snipe index' in a git repository to create an index"
		return check
	}

	indexPath := store.DefaultIndexPath(projectRoot)
	if !store.Exists(indexPath) {
		check.OK = false
		check.Message = "index not found"
		check.Details = fmt.Sprintf("Expected at: %s\nRun 'snipe index' to create", indexPath)
		return check
	}

	// Check freshness
	info, err := os.Stat(indexPath)
	if err != nil {
		check.OK = false
		check.Message = "could not read index file"
		return check
	}

	age := time.Since(info.ModTime())
	check.OK = true
	check.Message = "index found"

	if age > 24*time.Hour {
		check.Details = fmt.Sprintf("Path: %s\nLast updated: %s ago (consider running 'snipe index' to refresh)",
			indexPath, formatDuration(age))
	} else {
		check.Details = fmt.Sprintf("Path: %s\nLast updated: %s ago",
			indexPath, formatDuration(age))
	}

	return check
}

func findProjectRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
	return fmt.Sprintf("%.1f days", d.Hours()/24)
}
