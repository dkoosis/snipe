package internal

import (
	"os/exec"
)

// CheckRg verifies that ripgrep is installed
func CheckRg() error {
	_, err := exec.LookPath("rg")
	return err
}
