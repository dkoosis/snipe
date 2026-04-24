package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// OutputHuman renders one-line-per-result plain text for direct CLI use.
// TTY output is colorized; piped output is plain ASCII.
const OutputHuman OutputFormat = "human"

// ANSI escape sequences for terminal colors. Kept inline to avoid a color
// dependency — the codes are standard and fit D9 (no new abstraction towers).
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiYellow = "\x1b[33m"
)

// isStdoutTTY reports whether stdout is a terminal. Package-level indirection
// so tests can force colorless rendering without fiddling with fds.
var isStdoutTTY = func() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// writeHuman renders a response as one-line-per-result plain text.
// Only Response[Result] and Response[Summary] are handled here; other
// composite result types fall back to the Claude renderer — the human format
// is targeted at search/def/refs-style symbol lists where per-line output
// makes sense.
func (w *Writer) writeHuman(resp any) error {
	tty := isStdoutTTY()
	var b strings.Builder

	switch r := resp.(type) {
	case Response[Result]:
		if r.Error != nil {
			writeHumanError(&b, r.Error)
		} else {
			writeHumanResults(&b, r.Results, tty)
		}
	case Response[Summary]:
		writeHumanSummary(&b, r.Results)
	default:
		// Fallback: Claude-text format for composite result shapes.
		return w.writeClaude(resp)
	}

	_, err := io.WriteString(w.out, b.String())
	return err
}

func writeHumanResults(b *strings.Builder, results []Result, color bool) {
	if len(results) == 0 {
		b.WriteString("no results\n")
		return
	}
	for i := range results {
		writeHumanResultLine(b, &results[i], color)
	}
}

func writeHumanResultLine(b *strings.Builder, r *Result, color bool) {
	// <symbol>  <file>:<line>  <kind>
	name := r.Name
	if name == "" {
		name = r.Match
	}
	if r.Receiver != "" {
		name = "(" + r.Receiver + ")." + name
	}

	loc := r.File
	if r.Range.Start.Line > 0 {
		loc = fmt.Sprintf("%s:%d", r.File, r.Range.Start.Line)
	}

	if color {
		b.WriteString(ansiBold)
		b.WriteString(ansiCyan)
		b.WriteString(name)
		b.WriteString(ansiReset)
		b.WriteString("  ")
		b.WriteString(ansiDim)
		b.WriteString(loc)
		b.WriteString(ansiReset)
		if r.Kind != "" {
			b.WriteString("  ")
			b.WriteString(ansiYellow)
			b.WriteString(r.Kind)
			b.WriteString(ansiReset)
		}
	} else {
		b.WriteString(name)
		b.WriteString("  ")
		b.WriteString(loc)
		if r.Kind != "" {
			b.WriteString("  ")
			b.WriteString(r.Kind)
		}
	}
	b.WriteString("\n")
}

func writeHumanSummary(b *strings.Builder, results []Summary) {
	for _, s := range results {
		fmt.Fprintf(b, "%d results\n", s.Total)
		for _, f := range s.Files {
			fmt.Fprintf(b, "  %s: %d\n", f.File, f.Count)
		}
	}
}

func writeHumanError(b *strings.Builder, err *Error) {
	fmt.Fprintf(b, "error: %s\n", err.Message)
	for _, c := range err.Candidates {
		name := c.Name
		if c.Receiver != "" {
			name = "(" + c.Receiver + ")." + name
		}
		fmt.Fprintf(b, "  %s  %s  %s\n", name, c.File, c.Kind)
	}
}
