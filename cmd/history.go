package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/dkoosis/snipe/internal/metrics"
	"github.com/dkoosis/snipe/internal/output"
)

var (
	historyLimit int
)

func runHistory() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())

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

	jsonData, _ := json.MarshalIndent(entries, "", "  ")
	_, _ = os.Stdout.Write(jsonData)   // G104: stdout write for output
	_, _ = os.Stdout.WriteString("\n") // G104: stdout write for output

	return nil
}
