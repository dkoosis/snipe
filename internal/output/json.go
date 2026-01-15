package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/dkoosis/snipe/internal/util"
)

// globalFileCache is the default file cache for output operations.
var globalFileCache = util.NewFileCache(util.DefaultMaxCachedFiles)

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
	switch r := resp.(type) {
	case Response[Result]:
		return w.writeHumanResults(r)
	case Response[Summary]:
		return w.writeHumanSummary(r)
	default:
		// Fallback to JSON for unknown types
		return w.writeJSON(resp)
	}
}

func (w *Writer) writeHumanResults(resp Response[Result]) error {
	if resp.Error != nil {
		fmt.Fprintf(w.out, "Error: %s\n", resp.Error.Message)
		if len(resp.Error.Candidates) > 0 {
			fmt.Fprintln(w.out, "Candidates:")
			for _, c := range resp.Error.Candidates {
				fmt.Fprintf(w.out, "  %s (%s) in %s\n", c.Name, c.Kind, c.File)
			}
		}
		return nil
	}

	for _, r := range resp.Results {
		loc := fmt.Sprintf("%s:%d:%d", r.File, r.Range.Start.Line, r.Range.Start.Col)
		if r.Match != "" {
			fmt.Fprintf(w.out, "%s\t%s\t%s\n", loc, r.Kind, r.Match)
		} else {
			fmt.Fprintf(w.out, "%s\t%s\t%s\n", loc, r.Kind, r.Name)
		}
	}

	fmt.Fprintf(w.out, "\n%d results in %dms\n", resp.Meta.Total, resp.Meta.Ms)
	return nil
}

func (w *Writer) writeHumanSummary(resp Response[Summary]) error {
	if len(resp.Results) == 0 {
		fmt.Fprintln(w.out, "No results")
		return nil
	}

	s := resp.Results[0]
	fmt.Fprintf(w.out, "Total: %d results\n\n", s.Total)

	if len(s.Kinds) > 0 {
		fmt.Fprintln(w.out, "By kind:")
		for kind, count := range s.Kinds {
			fmt.Fprintf(w.out, "  %s: %d\n", kind, count)
		}
		fmt.Fprintln(w.out)
	}

	fmt.Fprintln(w.out, "By file:")
	for _, f := range s.Files {
		fmt.Fprintf(w.out, "  %s: %d\n", f.File, f.Count)
	}

	return nil
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

// EstimateTokens estimates the token count for a string.
//
// This uses a rough heuristic of ~4 characters per token, which is
// reasonably accurate for code (keywords, identifiers, operators).
// For LLM models like GPT-4 and Claude, actual tokenization varies,
// but this provides a useful upper-bound estimate for budget planning.
//
// Note: This is an approximation. For precise token counts, use the
// specific tokenizer for your target model.
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
	return globalFileCache.LoadLines(path)
}

// ClearFileCache clears the file cache. Call between commands if needed.
func ClearFileCache() {
	globalFileCache.Clear()
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
