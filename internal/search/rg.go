package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dkoosis/snipe/internal/output"
)

// rgMatch represents a ripgrep JSON match.
type rgMatch struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// rgMatchData represents the data field for a match type.
type rgMatchData struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	Lines struct {
		Text string `json:"text"`
	} `json:"lines"`
	LineNumber int          `json:"line_number"`
	Submatches []rgSubmatch `json:"submatches"`
}

// rgSubmatch represents a submatch within a line.
type rgSubmatch struct {
	Match struct {
		Text string `json:"text"`
	} `json:"match"`
	Start int `json:"start"`
	End   int `json:"end"`
}

// Search runs ripgrep and returns formatted results.
// Optional globs are passed as --glob flags to rg (e.g., "*.go", "store.go").
func Search(dir, pattern string, limit, contextLines int, globs ...string) ([]output.Result, error) {
	if _, err := exec.LookPath("rg"); err != nil {
		return nil, fmt.Errorf("ripgrep (rg) not found: install from https://github.com/BurntSushi/ripgrep")
	}

	if limit <= 0 {
		limit = 50
	}

	args := []string{
		"--json",
		"--line-number",
		"--column",
		"--no-follow",                          // Don't follow symlinks (avoids macOS network volume prompts)
		"--max-count", strconv.Itoa(limit * 2), // Get more than limit to account for context
	}

	if contextLines > 0 {
		args = append(args, "--context", strconv.Itoa(contextLines))
	}

	// Add exclude patterns
	excludes := []string{"vendor", "node_modules", ".git", "testdata"}
	for _, ex := range excludes {
		args = append(args, "--glob", "!"+ex)
	}

	// Add include globs if specified
	for _, g := range globs {
		if g != "" {
			args = append(args, "--glob", g)
		}
	}

	args = append(args, pattern, dir)

	cmd := exec.Command("rg", args...) // #nosec G204 -- args constructed internally, rg is trusted CLI tool
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start rg: %w", err)
	}

	var results []output.Result
	scanner := bufio.NewScanner(stdout)
	// Increase buffer size to handle long lines (default is 64KB, increase to 1MB)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() && len(results) < limit {
		line := scanner.Bytes()

		var msg rgMatch
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if msg.Type != "match" {
			continue
		}

		var data rgMatchData
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			continue
		}

		for _, sub := range data.Submatches {
			matchRange := output.Range{
				Start: output.Position{Line: data.LineNumber, Col: sub.Start + 1},
				End:   output.Position{Line: data.LineNumber, Col: sub.End + 1},
			}
			// Compute relative path for output
			filePathRel, _ := filepath.Rel(dir, data.Path.Text)
			if filePathRel == "" {
				filePathRel = data.Path.Text
			}
			result := output.Result{
				ID:         generateSearchID(data.LineNumber, sub.Start),
				File:       filePathRel,
				FileAbs:    data.Path.Text,
				Range:      matchRange,
				Kind:       "match",
				Name:       sub.Match.Text,
				Match:      strings.TrimSpace(data.Lines.Text),
				EditTarget: output.FormatEditTargetWithHash(filePathRel, data.Path.Text, matchRange),
			}
			results = append(results, result)

			if len(results) >= limit {
				break
			}
		}
	}

	// Check for scanner errors (e.g., line too long even with increased buffer)
	if err := scanner.Err(); err != nil {
		_ = stdout.Close() // G104: cleanup on error path
		_ = cmd.Wait()     // G104: cleanup on error path
		return results, fmt.Errorf("scan rg output: %w", err)
	}

	// Close pipe to signal rg we're done reading. This causes SIGPIPE on rg's
	// next write, allowing clean shutdown (same as `rg | head -n 50` behavior).
	_ = stdout.Close() // G104: intentional close to trigger SIGPIPE

	// Wait for rg to finish and check exit code.
	// rg exit codes: 0 = matches found, 1 = no matches, 2 = error.
	// Exit code -1 means rg was killed by a signal (e.g. SIGPIPE from our
	// early pipe close) — safe to ignore when we already have results.
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			switch code := exitErr.ExitCode(); {
			case code == 1:
				return results, nil
			case code == -1 && len(results) > 0:
				// Signal death (SIGPIPE) after we closed the pipe — expected
				return results, nil
			default:
				stderr := strings.TrimSpace(stderrBuf.String())
				if stderr != "" {
					return results, fmt.Errorf("rg error (exit %d): %s", code, stderr)
				}
				return results, fmt.Errorf("rg failed with exit code %d", code)
			}
		}
		return results, fmt.Errorf("wait for rg: %w", err)
	}

	return results, nil
}

func generateSearchID(line, col int) string {
	return "s" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}
