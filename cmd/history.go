package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/metrics"
	"github.com/dkoosis/snipe/internal/output"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show performance history over time",
	Long: `Shows performance trends from baseline history.

Reads from .snipe/metrics.jsonl to show how metrics have
changed across commits.

Examples:
  snipe history           # Show recent history
  snipe history --limit 5 # Show last 5 entries`,
	RunE: runHistory,
}

var (
	historyLimit int
)

func init() {
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 10, "Number of entries to show")
	rootCmd.AddCommand(historyCmd)
}

func runHistory(cmd *cobra.Command, args []string) error {
	_, human, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human)

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("history", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	historyFile := filepath.Join(dir, ".snipe", "metrics.jsonl")
	baselines, err := metrics.LoadHistory(historyFile)
	if err != nil {
		return w.WriteError("history", &output.Error{
			Code:    output.ErrNotFound,
			Message: "no history found at " + historyFile,
		})
	}

	if len(baselines) == 0 {
		return w.WriteError("history", &output.Error{
			Code:    output.ErrNotFound,
			Message: "history file is empty",
		})
	}

	// Limit entries
	if historyLimit > 0 && len(baselines) > historyLimit {
		baselines = baselines[len(baselines)-historyLimit:]
	}

	entries := metrics.ToHistoryEntries(baselines)

	if human {
		// Print table header
		fmt.Printf("%-20s %-8s %10s %8s %10s %10s\n",
			"Date", "Commit", "Index(ms)", "Symbols", "QueryP95", "DocCov")
		fmt.Println("─────────────────────────────────────────────────────────────────────────")

		for _, e := range entries {
			date := e.Timestamp[:10] // Just date part
			commit := e.GitCommit
			if len(commit) > 7 {
				commit = commit[:7]
			}

			// Format with deltas
			indexStr := fmt.Sprintf("%d", e.IndexMs)
			if e.IndexDelta != "" {
				indexStr = fmt.Sprintf("%d %s", e.IndexMs, e.IndexDelta)
			}

			queryStr := fmt.Sprintf("%.2fms", e.QueryP95Ms)
			if e.QueryDelta != "" {
				queryStr = fmt.Sprintf("%.2fms %s", e.QueryP95Ms, e.QueryDelta)
			}

			fmt.Printf("%-20s %-8s %10s %8d %10s %9.1f%%\n",
				date, commit, indexStr, e.Symbols, queryStr, e.DocCoverage)
		}
	} else {
		jsonData, _ := json.MarshalIndent(entries, "", "  ")
		os.Stdout.Write(jsonData)
		os.Stdout.Write([]byte("\n"))
	}

	return nil
}
