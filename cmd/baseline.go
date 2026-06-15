package cmd

import (
	"os"
	"path/filepath"

	"github.com/dkoosis/snipe/internal/metrics"
	"github.com/dkoosis/snipe/internal/output"
)

var (
	baselineOutput string
	baselineName   string
)

func runBaseline() error {
	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("baseline", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	// Use directory name as default baseline name
	name := baselineName
	if name == "" {
		name = filepath.Base(dir)
	}

	baseline, err := metrics.Capture(metrics.CaptureConfig{
		Dir:  dir,
		Name: name,
	})
	if err != nil {
		return w.WriteError("baseline", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to capture baseline: " + err.Error(),
		})
	}

	// Output JSON
	jsonData, err := baseline.ToJSON()
	if err != nil {
		return w.WriteError("baseline", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to serialize baseline: " + err.Error(),
		})
	}

	// Write to output file if specified
	outputFile := baselineOutput
	if outputFile == "" {
		outputFile = filepath.Join(dir, "BASELINE.json")
	}

	if err := os.WriteFile(outputFile, jsonData, 0600); err != nil { // #nosec G306 -- baseline is project data, not secrets
		return w.WriteError("baseline", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to write baseline file: " + err.Error(),
		})
	}

	// Append to history
	historyDir := filepath.Join(dir, ".snipe")
	if err := os.MkdirAll(historyDir, 0750); err == nil {
		historyFile := filepath.Join(historyDir, "metrics.jsonl")
		jsonl, _ := baseline.ToJSONL()
		if f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600); err == nil { // #nosec G304 -- path derived from cwd
			_, _ = f.Write(jsonl)      // G104: best-effort append to history
			_, _ = f.WriteString("\n") // G104: best-effort append
			_ = f.Close()              // G104: close in cleanup path
		}
	}

	_, _ = os.Stdout.Write(jsonData)   // G104: stdout write for output
	_, _ = os.Stdout.WriteString("\n") // G104: stdout write for output

	return nil
}
