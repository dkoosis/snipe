package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	Version   = "0.1.0"
	GitCommit = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("snipe version %s (commit: %s)\n", Version, GitCommit)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
