package cmd

import (
	"testing"
)

func TestLitsCmd_Registered(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "lits <value>" {
			return
		}
	}
	t.Error("lits command not registered")
}
