package cmd

import (
	"os"
	"path/filepath"

	"github.com/dkoosis/snipe/internal/metrics"
	"github.com/dkoosis/snipe/internal/output"
)

var (
	checkBaseline  string
	checkThreshold float64
	checkFailOnReg bool
)

func runCheck() error {
	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	dir, err := os.Getwd()
	if err != nil {
		return w.WriteError("check", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}

	// Load reference baseline
	baselinePath := checkBaseline
	if baselinePath == "" {
		baselinePath = filepath.Join(dir, "BASELINE.json")
	}

	reference, err := metrics.LoadBaseline(baselinePath)
	if err != nil {
		return w.WriteError("check", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to load baseline: " + err.Error(),
		})
	}

	// Capture current metrics
	current, err := metrics.Capture(metrics.CaptureConfig{
		Dir:  dir,
		Name: reference.Codebase.Name,
	})
	if err != nil {
		return w.WriteError("check", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to capture current metrics: " + err.Error(),
		})
	}

	// Compare
	comparison := metrics.Compare(current, reference, metrics.CompareConfig{
		Threshold: checkThreshold,
	})

	jsonData, _ := comparison.ToJSON()
	_, _ = os.Stdout.Write(jsonData)   // G104: stdout write for output
	_, _ = os.Stdout.WriteString("\n") // G104: stdout write for output

	if checkFailOnReg && comparison.HasFailure {
		os.Exit(1)
	}

	return nil
}
