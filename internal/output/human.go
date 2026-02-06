package output

import (
	"fmt"
	"io"

	"github.com/fatih/color"
)

// Color definitions used by human output formatters.
var (
	colorLine    = color.New(color.FgHiBlack)
	colorCount   = color.New(color.FgGreen)
	colorFresh   = color.New(color.FgGreen)
	colorStale   = color.New(color.FgYellow)
	colorMissing = color.New(color.FgRed)
)

// writeSymHuman writes a sym command result in human-readable format.
func (w *Writer) writeSymHuman(resp Response[SymResult]) error {
	return writeSymHumanNew(w.out, resp)
}

// writePackHuman writes a pack command result in human-readable format.
// Reuses sym human output since PackResult embeds the same structure.
func (w *Writer) writePackHuman(resp Response[PackResult]) error {
	if len(resp.Results) == 0 || resp.Results[0].Definition == nil {
		return w.writeJSON(resp)
	}
	pack := resp.Results[0]
	symResp := Response[SymResult]{
		Protocol: resp.Protocol,
		Ok:       resp.Ok,
		Results: []SymResult{{
			Definition:  pack.Definition,
			References:  pack.References,
			Callers:     pack.Callers,
			Callees:     pack.Callees,
			RefCount:    pack.RefCount,
			CallerCount: pack.CallerCount,
			CalleeCount: pack.CalleeCount,
		}},
		Meta:        resp.Meta,
		Error:       resp.Error,
		Suggestions: resp.Suggestions,
	}
	return writeSymHumanNew(w.out, symResp)
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
