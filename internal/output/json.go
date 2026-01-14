package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Writer handles output formatting
type Writer struct {
	out   io.Writer
	human bool
	start time.Time
}

// NewWriter creates a new output writer
func NewWriter(out io.Writer, human bool) *Writer {
	return &Writer{
		out:   out,
		human: human,
		start: time.Now(),
	}
}

// WriteResponse writes a response in JSON format
func (w *Writer) WriteResponse(resp any) error {
	if w.human {
		return w.writeHuman(resp)
	}
	return w.writeJSON(resp)
}

func (w *Writer) writeJSON(resp any) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	return enc.Encode(resp)
}

func (w *Writer) writeHuman(resp any) error {
	// For now, just pretty-print JSON
	// TODO: implement proper human-readable format
	return w.writeJSON(resp)
}

// WriteError writes an error response
func (w *Writer) WriteError(command string, err *Error) error {
	resp := Response[any]{
		Results: nil,
		Meta: Meta{
			Command: command,
			Ms:      time.Since(w.start).Milliseconds(),
		},
		Error: err,
	}
	return w.WriteResponse(resp)
}

// Elapsed returns milliseconds since writer creation
func (w *Writer) Elapsed() int64 {
	return time.Since(w.start).Milliseconds()
}

// EstimateTokens estimates the token count for a string
// Rough approximation: ~4 chars per token for code
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// FormatEditTarget formats a range as an edit target string
func FormatEditTarget(file string, r Range) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d",
		file,
		r.Start.Line, r.Start.Col,
		r.End.Line, r.End.Col,
	)
}

// AddContext loads N lines of context before and after the result's range
func AddContext(result *Result, n int) error {
	if n <= 0 {
		return nil
	}

	lines, err := readFileLines(result.File)
	if err != nil {
		return err
	}

	startLine := result.Range.Start.Line
	endLine := result.Range.End.Line

	// Get N lines before
	beforeStart := max(1, startLine-n)
	var before []string
	for i := beforeStart; i < startLine; i++ {
		if i <= len(lines) {
			before = append(before, lines[i-1])
		}
	}

	// Get N lines after
	afterEnd := min(len(lines), endLine+n)
	var after []string
	for i := endLine + 1; i <= afterEnd; i++ {
		if i <= len(lines) {
			after = append(after, lines[i-1])
		}
	}

	if len(before) > 0 || len(after) > 0 {
		result.Context = &Context{
			Before: before,
			After:  after,
		}
	}

	return nil
}

func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	// Increase buffer for long lines (minified code, long strings)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// BuildSummary creates a summary from a slice of results
func BuildSummary(results []Result) Summary {
	fileCounts := make(map[string]int)
	kindCounts := make(map[string]int)

	for _, r := range results {
		fileCounts[r.File]++
		if r.Kind != "" {
			kindCounts[r.Kind]++
		}
	}

	var files []FileSummary
	for file, count := range fileCounts {
		files = append(files, FileSummary{File: file, Count: count})
	}

	return Summary{
		Total: len(results),
		Files: files,
		Kinds: kindCounts,
	}
}

// AddBody extracts the full source code for a result based on its range.
func AddBody(result *Result) error {
	lines, err := readFileLines(result.File)
	if err != nil {
		return err
	}

	startLine := result.Range.Start.Line
	endLine := result.Range.End.Line

	if startLine < 1 || endLine > len(lines) {
		return nil // Invalid range, skip
	}

	// Extract lines from startLine to endLine (1-indexed)
	var body string
	for i := startLine; i <= endLine && i <= len(lines); i++ {
		if i > startLine {
			body += "\n"
		}
		body += lines[i-1]
	}

	result.Body = body
	return nil
}
