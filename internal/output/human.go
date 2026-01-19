package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
)

// Color definitions
var (
	colorKind     = color.New(color.FgCyan)
	colorFile     = color.New(color.FgBlue)
	colorReceiver = color.New(color.FgYellow)
	colorName     = color.New(color.FgWhite, color.Bold)
	colorLine     = color.New(color.FgHiBlack)
	colorCount    = color.New(color.FgGreen)
	colorHeader   = color.New(color.FgHiBlack)
	colorFresh    = color.New(color.FgGreen)
	colorStale    = color.New(color.FgYellow)
	colorMissing  = color.New(color.FgRed)
)

// writeSymHuman writes a sym command result in human-readable format.
func (w *Writer) writeSymHuman(resp Response[SymResult]) error {
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

	if len(resp.Results) == 0 {
		fmt.Fprintln(w.out, "No results")
		return nil
	}

	sym := resp.Results[0]
	def := sym.Definition
	if def == nil {
		fmt.Fprintln(w.out, "No definition found")
		return nil
	}

	// Header: Name + Kind
	writeSymHeader(w.out, def)

	// Separator
	writeSeparator(w.out)

	// Body (source code)
	if def.Body != "" {
		fmt.Fprintln(w.out, def.Body)
		writeSeparator(w.out)
	}

	// Stats line
	writeSymStats(w.out, sym.RefCount, sym.CallerCount, sym.CalleeCount)

	// Callers preview
	if len(sym.Callers) > 0 {
		writeCallersList(w.out, "Callers", sym.Callers, sym.CallerCount)
	}

	// Callees list
	if len(sym.Callees) > 0 {
		writeCalleesList(w.out, "Callees", sym.Callees, sym.CalleeCount)
	}

	return nil
}

func writeSymHeader(out io.Writer, def *Result) {
	// Line 1: Name (colored) + kind (right-aligned)
	name := def.Name
	kind := def.Kind

	// Format: "Name                                        kind"
	// with receiver if present
	namePart := colorName.Sprint(name)
	if def.Receiver != "" {
		namePart = colorName.Sprint(name) + " " + colorReceiver.Sprintf("(%s)", def.Receiver)
	}

	// Right-align kind
	kindPart := colorKind.Sprint(kind)
	fmt.Fprintf(out, "%s%s%s\n", namePart, strings.Repeat(" ", max(1, 52-len(name)-len(def.Receiver))), kindPart)

	// Line 2: file:line-line  (receiver if method)
	loc := fmt.Sprintf("%s:%d-%d", def.File, def.Range.Start.Line, def.Range.End.Line)
	recvPart := ""
	if def.Receiver != "" {
		recvPart = "  " + colorReceiver.Sprintf("(%s)", def.Receiver)
	}
	fmt.Fprintf(out, "%s%s\n", colorFile.Sprint(loc), recvPart)
}

func writeSeparator(out io.Writer) {
	fmt.Fprintln(out, colorLine.Sprint(strings.Repeat("─", 56)))
}

func writeSymStats(out io.Writer, refs, callers, callees int) {
	parts := []string{}

	if refs >= 0 {
		parts = append(parts, colorCount.Sprintf("%d", refs)+" refs")
	}
	if callers >= 0 {
		parts = append(parts, colorCount.Sprintf("%d", callers)+" callers")
	}
	if callees >= 0 {
		parts = append(parts, colorCount.Sprintf("%d", callees)+" callees")
	}

	if len(parts) > 0 {
		fmt.Fprintln(out, strings.Join(parts, " │ "))
		fmt.Fprintln(out)
	}
}

func writeCallersList(out io.Writer, label string, items []Result, total int) {
	if len(items) == 0 {
		return
	}

	if total > len(items) {
		fmt.Fprintf(out, "%s (showing %d of %d):\n",
			colorHeader.Sprint(label), len(items), total)
	} else {
		fmt.Fprintf(out, "%s:\n", colorHeader.Sprint(label))
	}

	for _, item := range items {
		name := item.Name
		file := item.File
		line := item.Range.Start.Line
		fmt.Fprintf(out, "  %-16s %s:%d\n", name, colorFile.Sprint(file), line)
	}
	fmt.Fprintln(out)
}

func writeCalleesList(out io.Writer, label string, items []Result, total int) {
	if len(items) == 0 {
		return
	}

	if total > len(items) {
		fmt.Fprintf(out, "%s (showing %d of %d):\n",
			colorHeader.Sprint(label), len(items), total)
	} else {
		fmt.Fprintf(out, "%s:\n", colorHeader.Sprint(label))
	}

	for _, item := range items {
		name := item.Name
		file := item.File
		line := item.Range.Start.Line
		fmt.Fprintf(out, "  %-16s %s:%d\n", name, colorFile.Sprint(file), line)
	}
}

// StatusInfo holds information for the status command.
type StatusInfo struct {
	State       IndexState
	Commit      string
	IndexedAt   string
	Symbols     int
	Refs        int
	Calls       int
	Fingerprint string
	AgeDesc     string
}

// WriteStatusHuman writes index status in human-readable format.
func WriteStatusHuman(out io.Writer, info StatusInfo) {
	// Line 1: state + commit + age
	var stateColor *color.Color
	switch info.State {
	case IndexFresh:
		stateColor = colorFresh
	case IndexStale:
		stateColor = colorStale
	case IndexMissing, IndexNotUsed:
		stateColor = colorMissing
	}

	statePart := stateColor.Sprint(info.State)
	commitPart := ""
	if info.Commit != "" {
		// Truncate commit to 7 chars
		commit := info.Commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		commitPart = fmt.Sprintf(" (%s", commit)
		if info.AgeDesc != "" {
			commitPart += ", " + info.AgeDesc
		}
		commitPart += ")"
	}

	fmt.Fprintf(out, "snipe index: %s%s\n", statePart, colorLine.Sprint(commitPart))

	// Line 2: stats
	if info.Symbols > 0 || info.Refs > 0 || info.Calls > 0 {
		fmt.Fprintf(out, "  symbols: %s  refs: %s  calls: %s\n",
			colorCount.Sprintf("%d", info.Symbols),
			colorCount.Sprintf("%d", info.Refs),
			colorCount.Sprintf("%d", info.Calls))
	}
}
