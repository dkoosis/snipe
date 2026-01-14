package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Writer handles output formatting
type Writer struct {
	out    io.Writer
	human  bool
	start  time.Time
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
