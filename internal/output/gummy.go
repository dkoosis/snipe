// Gummy provides beautiful human-readable CLI output for snipe.
//
// Design principles:
//   - Information hierarchy: most important info first, visually prominent
//   - Scannable: headers, separators, and whitespace guide the eye
//   - Color with purpose: color encodes meaning, not decoration
//   - Consistent patterns: same visual language across all commands

package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
)

// Layout constants
const (
	gummyWidth       = 72 // Default output width
	gummyMaxResults  = 20 // Default max results before truncation
	gummyIndentSize  = 2  // Spaces per indent level
	gummyBodyPreview = 15 // Lines of source code to show
	gummyListPreview = 5  // Items in caller/callee lists
)

// Gummy semantic colors - each color has a specific meaning
var (
	// Primary elements
	gummyName     = color.New(color.FgWhite, color.Bold) // Main focus (symbol names)
	gummyKind     = color.New(color.FgCyan)              // Categories (func, type, method)
	gummyFile     = color.New(color.FgBlue)              // File paths for navigation
	gummyReceiver = color.New(color.FgYellow)            // Method receiver context

	// Metrics and counts
	gummyCount = color.New(color.FgGreen) // Positive metrics (refs, callers)

	// Status indicators
	gummyFresh   = color.New(color.FgGreen)  // Good state
	gummyStale   = color.New(color.FgYellow) // Needs attention
	gummyMissing = color.New(color.FgRed)    // Problems/errors

	// Secondary elements
	gummyDim   = color.New(color.FgHiBlack) // Line numbers, separators, hints
	gummyMatch = color.New(color.FgMagenta) // Search match highlights
	gummyID    = color.New(color.FgHiBlack) // Hex IDs (secondary info)

	// Labels
	gummyLabel = color.New(color.FgHiBlack) // Section labels
)

// Glyph constants for visual elements
const (
	gummySep      = "─"
	gummyArrow    = "→"
	gummyCheck    = "✓"
	gummyCross    = "✗"
	gummyEllipsis = "..."
	gummyPipe     = "│"
)

// gummyWriter wraps an io.Writer with formatting helpers
type gummyWriter struct {
	out   io.Writer
	width int
}

// newGummyWriter creates a new gummy writer
func newGummyWriter(out io.Writer) *gummyWriter {
	return &gummyWriter{out: out, width: gummyWidth}
}

// header writes a header line with title and optional right-aligned info
func (w *gummyWriter) header(title, right string) {
	titleLen := len(title)
	rightLen := len(right)
	padding := w.width - titleLen - rightLen
	if padding < 1 {
		padding = 1
	}
	fmt.Fprintf(w.out, "%s%s%s\n", gummyName.Sprint(title), strings.Repeat(" ", padding), gummyKind.Sprint(right))
}

// headerWithReceiver writes a header with receiver annotation
func (w *gummyWriter) headerWithReceiver(title, recv, right string) {
	if recv != "" {
		namePart := gummyName.Sprint(title) + " " + gummyReceiver.Sprintf("(%s)", recv)
		padding := w.width - len(title) - len(recv) - 3 - len(right)
		if padding < 1 {
			padding = 1
		}
		fmt.Fprintf(w.out, "%s%s%s\n", namePart, strings.Repeat(" ", padding), gummyKind.Sprint(right))
	} else {
		w.header(title, right)
	}
}

// subheader writes a secondary header (file path)
func (w *gummyWriter) subheader(text string) {
	fmt.Fprintln(w.out, gummyFile.Sprint(text))
}

// separator writes a horizontal line
func (w *gummyWriter) separator() {
	fmt.Fprintln(w.out, gummyDim.Sprint(strings.Repeat(gummySep, w.width)))
}

// blank writes an empty line
func (w *gummyWriter) blank() {
	fmt.Fprintln(w.out)
}

// line writes a plain line of text
func (w *gummyWriter) line(text string) {
	fmt.Fprintln(w.out, text)
}

// body writes source code body (preserves formatting)
func (w *gummyWriter) body(code string) {
	if code == "" {
		return
	}
	fmt.Fprintln(w.out, code)
}

// stats writes a stats line with pipe separators
func (w *gummyWriter) stats(parts ...string) {
	if len(parts) == 0 {
		return
	}
	fmt.Fprintln(w.out, strings.Join(parts, " "+gummyDim.Sprint(gummyPipe)+" "))
}

// statItem formats a count with label
func gummyStatItem(count int, label string) string {
	return gummyCount.Sprintf("%d", count) + " " + label
}

// footer writes a footer line with left and optional right parts
func (w *gummyWriter) footer(left, right string) {
	if right == "" {
		fmt.Fprintln(w.out, gummyDim.Sprint(left))
		return
	}
	padding := w.width - len(left) - len(right)
	if padding < 1 {
		padding = 1
	}
	fmt.Fprintf(w.out, "%s%s%s\n", gummyDim.Sprint(left), strings.Repeat(" ", padding), gummyDim.Sprint(right))
}

// sectionHeader writes a section label
func (w *gummyWriter) sectionHeader(label string, showing, total int) {
	if total > showing && showing > 0 {
		fmt.Fprintf(w.out, "%s %s:\n", gummyLabel.Sprint(label), gummyDim.Sprintf("(showing %d of %d)", showing, total))
	} else {
		fmt.Fprintf(w.out, "%s:\n", gummyLabel.Sprint(label))
	}
}

// listItem writes an indented list item with name and location
func (w *gummyWriter) listItem(name, file string, line int) {
	nameWidth := 20
	namePart := name
	if len(namePart) > nameWidth {
		namePart = namePart[:nameWidth-3] + gummyEllipsis
	}
	fmt.Fprintf(w.out, "  %-*s  %s:%d\n", nameWidth, namePart, gummyFile.Sprint(file), line)
}

// listItemWithCount writes a list item with call count indicator
func (w *gummyWriter) listItemWithCount(name, file string, line, count int) {
	nameWidth := 20
	namePart := name
	if len(namePart) > nameWidth {
		namePart = namePart[:nameWidth-3] + gummyEllipsis
	}
	countPart := ""
	if count > 1 {
		countPart = gummyDim.Sprintf(" (%d×)", count)
	}
	fmt.Fprintf(w.out, "  %-*s  %s:%d%s\n", nameWidth, namePart, gummyFile.Sprint(file), line, countPart)
}

// fileGroup writes a file header for grouped results
func (w *gummyWriter) fileGroup(file string, count int) {
	fmt.Fprintf(w.out, "%s %s\n", gummyFile.Sprint(file), gummyDim.Sprintf("(%d)", count))
}

// matchLine writes a line with line number and content
func (w *gummyWriter) matchLine(lineNum int, content string) {
	fmt.Fprintf(w.out, "  %s  %s\n", gummyDim.Sprintf(":%d", lineNum), content)
}

// errorWithHint writes an error with a suggested action
func (w *gummyWriter) errorWithHint(msg, hint string) {
	fmt.Fprintln(w.out, gummyMissing.Sprint("Error: ")+msg)
	if hint != "" {
		fmt.Fprintln(w.out, gummyDim.Sprint("  "+gummyArrow+" "+hint))
	}
}

// candidates writes a list of ambiguous symbol candidates
func (w *gummyWriter) candidates(candidates []Candidate) {
	if len(candidates) == 0 {
		return
	}
	w.blank()
	fmt.Fprintln(w.out, gummyLabel.Sprint("Did you mean:"))
	for _, c := range candidates {
		fmt.Fprintf(w.out, "  %s %s  %s\n",
			gummyName.Sprint(c.Name),
			gummyKind.Sprint(c.Kind),
			gummyFile.Sprint(c.File))
	}
}

// moreIndicator writes a truncation indicator
func (w *gummyWriter) moreIndicator(count int, context string) {
	if count <= 0 {
		return
	}
	fmt.Fprintf(w.out, "  %s %d more%s\n", gummyDim.Sprint(gummyEllipsis), count, context)
}

// keyValue writes a key-value pair with alignment
func (w *gummyWriter) keyValue(key string, value any) {
	keyWidth := 12
	fmt.Fprintf(w.out, "  %-*s  %v\n", keyWidth, key, value)
}

// keyValueColored writes a key-value pair with colored value
func (w *gummyWriter) keyValueColored(key string, value int) {
	keyWidth := 12
	fmt.Fprintf(w.out, "  %-*s  %s\n", keyWidth, key, gummyCount.Sprint(humanize.Comma(int64(value))))
}

// gummyHighlight applies match highlighting to text
func gummyHighlight(text, pattern string) string {
	if pattern == "" {
		return text
	}
	lower := strings.ToLower(text)
	patternLower := strings.ToLower(pattern)
	idx := strings.Index(lower, patternLower)
	if idx < 0 {
		return text
	}
	return text[:idx] + gummyMatch.Sprint(text[idx:idx+len(pattern)]) + text[idx+len(pattern):]
}

// gummyFormatDuration formats milliseconds as human-readable
func gummyFormatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// gummyTruncateLines truncates a multi-line string to maxLines.
func gummyTruncateLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n" + gummyDim.Sprint("  // ...")
}

// gummyFormatLocation formats file:start-end
func gummyFormatLocation(file string, start, end int) string {
	if start == end {
		return fmt.Sprintf("%s:%d", file, start)
	}
	return fmt.Sprintf("%s:%d-%d", file, start, end)
}

// gummyPadToWidth returns spaces to pad between left and right to fill width.
//
//nolint:unparam // width kept as param for flexibility
func gummyPadToWidth(leftLen, rightLen, width int) string {
	padding := width - leftLen - rightLen
	if padding < 1 {
		padding = 1
	}
	return strings.Repeat(" ", padding)
}

// ============================================================================
// Command Formatters
// ============================================================================

// writeDefHuman formats a def command response in human-readable format
func writeDefHuman(out io.Writer, resp Response[Result]) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	if len(resp.Results) == 0 {
		w.line("No definition found")
		return nil
	}

	def := resp.Results[0]

	// Header: Name + Kind
	w.headerWithReceiver(def.Name, def.Receiver, def.Kind)

	// Location
	loc := gummyFormatLocation(def.File, def.Range.Start.Line, def.Range.End.Line)
	w.subheader(loc)

	// Separator before body
	w.separator()

	// Body (source code)
	if def.Body != "" {
		body := gummyTruncateLines(def.Body, gummyBodyPreview)
		w.body(body)
		w.separator()
	}

	// Stats line
	var stats []string
	if def.RefCount >= 0 {
		stats = append(stats, gummyStatItem(def.RefCount, "refs"))
	}
	if len(def.CallersPreview) > 0 {
		stats = append(stats, gummyStatItem(len(def.CallersPreview), "callers"))
	}
	if len(stats) > 0 {
		w.stats(stats...)
	}

	// Callers preview if available
	if len(def.CallersPreview) > 0 {
		w.blank()
		w.sectionHeader("Callers", len(def.CallersPreview), len(def.CallersPreview))
		for _, c := range def.CallersPreview {
			w.listItem(c.Name, c.File, c.Line)
		}
	}

	// Footer with timing and ID
	w.blank()
	footer := gummyFormatDuration(resp.Meta.Ms)
	idPart := ""
	if def.ID != "" {
		idPart = "id: " + def.ID
	}
	w.footer(footer, idPart)

	return nil
}

// writeRefsHuman formats a refs command response in human-readable format
func writeRefsHuman(out io.Writer, resp Response[Result], symbol string) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	total := resp.Meta.Total
	if total == 0 {
		total = len(resp.Results)
	}

	// Header
	title := fmt.Sprintf("References to %s", gummyName.Sprint(symbol))
	right := fmt.Sprintf("%d total", total)
	fmt.Fprintf(out, "%s%s%s\n", title,
		gummyPadToWidth(len("References to ")+len(symbol), len(right), gummyWidth),
		gummyCount.Sprint(right))

	w.separator()
	w.blank()

	if len(resp.Results) == 0 {
		w.line("  No references found")
		w.blank()
		w.separator()
		w.footer(gummyFormatDuration(resp.Meta.Ms), "")
		return nil
	}

	// Group by file
	groups := gummyGroupByFile(resp.Results)

	// Sort files by count (most refs first)
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].results) > len(groups[j].results)
	})

	// Show up to MaxResults total
	shown := 0
	filesShown := 0
	for _, g := range groups {
		if shown >= gummyMaxResults {
			break
		}

		w.fileGroup(g.file, len(g.results))

		for _, r := range g.results {
			if shown >= gummyMaxResults {
				break
			}
			content := r.Match
			if content == "" {
				content = r.Name
			}
			if symbol != "" {
				content = gummyHighlight(content, symbol)
			}
			w.matchLine(r.Range.Start.Line, content)
			shown++
		}
		w.blank()
		filesShown++
	}

	// Truncation indicator
	if shown < total {
		remaining := total - shown
		remainingFiles := len(groups) - filesShown
		context := ""
		if remainingFiles > 0 {
			context = fmt.Sprintf(" in %d files", remainingFiles)
		}
		w.moreIndicator(remaining, context)
		w.blank()
	}

	w.separator()
	w.footer(fmt.Sprintf("%d results in %s", total, gummyFormatDuration(resp.Meta.Ms)), "")

	return nil
}

// writeCallersHuman formats a callers command response in human-readable format
func writeCallersHuman(out io.Writer, resp Response[Result], symbol, targetID string) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	total := resp.Meta.Total
	if total == 0 {
		total = len(resp.Results)
	}

	// Header
	title := fmt.Sprintf("Callers of %s", gummyName.Sprint(symbol))
	right := fmt.Sprintf("%d callers", total)
	fmt.Fprintf(out, "%s%s%s\n", title,
		gummyPadToWidth(len("Callers of ")+len(symbol), len(right), gummyWidth),
		gummyCount.Sprint(right))

	w.separator()
	w.blank()

	if len(resp.Results) == 0 {
		w.line("  No callers found")
		w.blank()
		w.separator()
		w.footer(gummyFormatDuration(resp.Meta.Ms), "")
		return nil
	}

	// Aggregate callers by function name
	callerAgg := gummyAggregateCallers(resp.Results)

	// Sort by call count (most calls first)
	sort.Slice(callerAgg, func(i, j int) bool {
		return callerAgg[i].count > callerAgg[j].count
	})

	// Display callers
	shown := 0
	totalCallSites := 0
	for _, c := range callerAgg {
		if shown >= gummyMaxResults {
			break
		}
		w.listItemWithCount(c.name, c.file, c.line, c.count)
		totalCallSites += c.count
		shown++
	}

	w.blank()

	// Truncation indicator
	if shown < len(callerAgg) {
		w.moreIndicator(len(callerAgg)-shown, "")
		w.blank()
	}

	w.separator()

	// Footer
	footerLeft := fmt.Sprintf("%d callers", len(callerAgg))
	if totalCallSites > len(callerAgg) {
		footerLeft += fmt.Sprintf(", %d call sites", totalCallSites)
	}
	footerLeft += " " + gummyDim.Sprint(gummyPipe) + " " + gummyFormatDuration(resp.Meta.Ms)

	idPart := ""
	if targetID != "" {
		idPart = "id: " + targetID
	}
	w.footer(footerLeft, idPart)

	return nil
}

// writeCalleesHuman formats a callees command response in human-readable format
func writeCalleesHuman(out io.Writer, resp Response[Result], symbol, targetID string) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	total := resp.Meta.Total
	if total == 0 {
		total = len(resp.Results)
	}

	// Header
	title := fmt.Sprintf("Callees of %s", gummyName.Sprint(symbol))
	right := fmt.Sprintf("%d callees", total)
	fmt.Fprintf(out, "%s%s%s\n", title,
		gummyPadToWidth(len("Callees of ")+len(symbol), len(right), gummyWidth),
		gummyCount.Sprint(right))

	w.separator()
	w.blank()

	if len(resp.Results) == 0 {
		w.line("  No callees found")
		w.blank()
		w.separator()
		w.footer(gummyFormatDuration(resp.Meta.Ms), "")
		return nil
	}

	// Aggregate callees by function name
	calleeAgg := gummyAggregateCallees(resp.Results)

	// Sort by call count (most calls first), then alphabetically
	sort.Slice(calleeAgg, func(i, j int) bool {
		if len(calleeAgg[i].lines) != len(calleeAgg[j].lines) {
			return len(calleeAgg[i].lines) > len(calleeAgg[j].lines)
		}
		return calleeAgg[i].name < calleeAgg[j].name
	})

	// Display callees
	shown := 0
	for _, c := range calleeAgg {
		if shown >= gummyMaxResults {
			break
		}
		gummyWriteCalleeItem(w, c)
		shown++
	}

	w.blank()

	// Truncation indicator
	if shown < len(calleeAgg) {
		w.moreIndicator(len(calleeAgg)-shown, "")
		w.blank()
	}

	w.separator()

	// Footer
	footerLeft := fmt.Sprintf("%d unique callees", len(calleeAgg))
	footerLeft += " " + gummyDim.Sprint(gummyPipe) + " " + gummyFormatDuration(resp.Meta.Ms)

	idPart := ""
	if targetID != "" {
		idPart = "id: " + targetID
	}
	w.footer(footerLeft, idPart)

	return nil
}

// writeSearchHuman formats a search command response in human-readable format
func writeSearchHuman(out io.Writer, resp Response[Result], pattern string) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	total := resp.Meta.Total
	if total == 0 {
		total = len(resp.Results)
	}

	// Header
	quotedPattern := fmt.Sprintf("\"%s\"", pattern)
	title := fmt.Sprintf("Search: %s", gummyMatch.Sprint(quotedPattern))
	right := fmt.Sprintf("%d matches", total)
	titleLen := len("Search: ") + len(pattern) + 2
	fmt.Fprintf(out, "%s%s%s\n", title,
		gummyPadToWidth(titleLen, len(right), gummyWidth),
		gummyCount.Sprint(right))

	w.separator()
	w.blank()

	if len(resp.Results) == 0 {
		w.line("  No matches found")
		w.blank()
		w.separator()
		w.footer(gummyFormatDuration(resp.Meta.Ms), "")
		return nil
	}

	// Group by file
	groups := gummyGroupByFile(resp.Results)

	// Sort files by count (most matches first)
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].results) > len(groups[j].results)
	})

	// Show up to MaxResults total
	shown := 0
	filesShown := 0
	for _, g := range groups {
		if shown >= gummyMaxResults {
			break
		}

		fmt.Fprintln(out, gummyFile.Sprint(g.file))

		for _, r := range g.results {
			if shown >= gummyMaxResults {
				break
			}
			content := r.Match
			if content == "" {
				content = r.Name
			}
			if pattern != "" {
				content = gummyHighlight(content, pattern)
			}
			w.matchLine(r.Range.Start.Line, content)
			shown++
		}
		w.blank()
		filesShown++
	}

	// Truncation indicator
	if shown < total {
		remaining := total - shown
		remainingFiles := len(groups) - filesShown
		context := ""
		if remainingFiles > 0 {
			context = fmt.Sprintf(" in %d files", remainingFiles)
		}
		w.moreIndicator(remaining, context)
		w.blank()
	}

	w.separator()
	w.footer(fmt.Sprintf("%d matches %s %s", total, gummyDim.Sprint(gummyPipe), gummyFormatDuration(resp.Meta.Ms)), "")

	return nil
}

// writeShowHuman formats a show command response in human-readable format
func writeShowHuman(out io.Writer, resp Response[Result]) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	if len(resp.Results) == 0 {
		w.line("No symbol found")
		return nil
	}

	r := resp.Results[0]

	// Header: ID → Name + Kind
	idPart := ""
	if r.ID != "" {
		truncatedID := r.ID
		if len(truncatedID) > 16 {
			truncatedID = truncatedID[:16]
		}
		idPart = gummyID.Sprint(truncatedID) + " " + gummyDim.Sprint(gummyArrow) + " "
	}

	namePart := gummyName.Sprint(r.Name)
	if r.Receiver != "" {
		namePart += " " + gummyReceiver.Sprintf("(%s)", r.Receiver)
	}

	kindPart := gummyKind.Sprint(r.Kind)

	// Calculate padding
	baseLen := len(r.ID)
	if baseLen > 16 {
		baseLen = 16
	}
	baseLen += 3 + len(r.Name)
	if r.Receiver != "" {
		baseLen += len(r.Receiver) + 3
	}
	padding := gummyWidth - baseLen - len(r.Kind)
	if padding < 1 {
		padding = 1
	}

	fmt.Fprintf(out, "%s%s%s%s\n", idPart, namePart, strings.Repeat(" ", padding), kindPart)

	// Location
	loc := gummyFormatLocation(r.File, r.Range.Start.Line, r.Range.End.Line)
	w.subheader(loc)

	// Separator
	w.separator()

	// Body (source code)
	if r.Body != "" {
		body := gummyTruncateLines(r.Body, gummyBodyPreview)
		w.body(body)
		w.separator()
	}

	// Stats line
	var stats []string
	if r.RefCount >= 0 {
		stats = append(stats, gummyStatItem(r.RefCount, "refs"))
	}
	if len(r.CallersPreview) > 0 {
		stats = append(stats, gummyStatItem(len(r.CallersPreview), "callers"))
	}
	if len(stats) > 0 {
		w.stats(stats...)
	}

	// Callers preview
	if len(r.CallersPreview) > 0 {
		w.blank()
		w.sectionHeader("Callers", len(r.CallersPreview), len(r.CallersPreview))
		for i, c := range r.CallersPreview {
			if i >= gummyListPreview {
				w.moreIndicator(len(r.CallersPreview)-gummyListPreview, "")
				break
			}
			w.listItem(c.Name, c.File, c.Line)
		}
	}

	// Siblings
	if len(r.Siblings) > 0 {
		w.blank()
		w.sectionHeader("Siblings", len(r.Siblings), len(r.Siblings))
		for i, s := range r.Siblings {
			if i >= gummyListPreview {
				w.moreIndicator(len(r.Siblings)-gummyListPreview, "")
				break
			}
			fmt.Fprintf(out, "  %-20s  %s  %s:%d\n",
				s.Name,
				gummyKind.Sprint(s.Kind),
				gummyFile.Sprint(r.File),
				s.Line)
		}
	}

	// Hints
	if len(r.Hints) > 0 || len(r.KGHints) > 0 {
		w.blank()
		w.separator()
		gummyWriteHints(w, r.Hints, r.KGHints)
	}

	// Footer
	w.blank()
	w.footer(gummyFormatDuration(resp.Meta.Ms), "")

	return nil
}

// writeSymHumanNew formats a sym command response in human-readable format
func writeSymHumanNew(out io.Writer, resp Response[SymResult]) error {
	w := newGummyWriter(out)

	if resp.Error != nil {
		return writeGummyError(w, resp.Error)
	}

	if len(resp.Results) == 0 {
		w.line("No results")
		return nil
	}

	sym := resp.Results[0]
	def := sym.Definition
	if def == nil {
		w.line("No definition found")
		return nil
	}

	// Header: Name + Kind
	w.headerWithReceiver(def.Name, def.Receiver, def.Kind)

	// Location
	loc := gummyFormatLocation(def.File, def.Range.Start.Line, def.Range.End.Line)
	w.subheader(loc)

	// Separator
	w.separator()

	// Body (source code)
	if def.Body != "" {
		body := gummyTruncateLines(def.Body, gummyBodyPreview)
		w.body(body)
		w.separator()
	}

	// Stats line
	var stats []string
	if sym.RefCount >= 0 {
		stats = append(stats, gummyStatItem(sym.RefCount, "refs"))
	}
	if sym.CallerCount >= 0 {
		stats = append(stats, gummyStatItem(sym.CallerCount, "callers"))
	}
	if sym.CalleeCount >= 0 {
		stats = append(stats, gummyStatItem(sym.CalleeCount, "callees"))
	}
	if len(stats) > 0 {
		w.stats(stats...)
	}

	// Callers section
	if len(sym.Callers) > 0 {
		w.blank()
		showingCallers := min(len(sym.Callers), gummyListPreview)
		w.sectionHeader("Callers", showingCallers, sym.CallerCount)
		for i, c := range sym.Callers {
			if i >= gummyListPreview {
				remaining := sym.CallerCount - gummyListPreview
				w.moreIndicator(remaining, "")
				break
			}
			w.listItem(c.Name, c.File, c.Range.Start.Line)
		}
	}

	// Callees section
	if len(sym.Callees) > 0 {
		w.blank()
		showingCallees := min(len(sym.Callees), gummyListPreview)
		w.sectionHeader("Callees", showingCallees, sym.CalleeCount)
		for i, c := range sym.Callees {
			if i >= gummyListPreview {
				remaining := sym.CalleeCount - gummyListPreview
				w.moreIndicator(remaining, "")
				break
			}
			w.listItem(c.Name, c.File, c.Range.Start.Line)
		}
	}

	// Hints
	if len(def.Hints) > 0 || len(def.KGHints) > 0 {
		w.blank()
		w.separator()
		gummyWriteHints(w, def.Hints, def.KGHints)
	}

	// Footer
	w.blank()
	idPart := ""
	if def.ID != "" {
		idPart = "id: " + def.ID
	}
	w.footer(gummyFormatDuration(resp.Meta.Ms), idPart)

	return nil
}

// ============================================================================
// Status Formatter
// ============================================================================

// GummyStatusInfo holds information for the status command
type GummyStatusInfo struct {
	State       string // fresh, stale, missing
	Commit      string
	IndexedAt   string
	AgeDesc     string
	Symbols     int
	Refs        int
	Calls       int
	Embedded    int // Number of symbols with embeddings
	Enriched    int // Number of symbols with LLM enrichment
	Fingerprint string
}

// WriteGummyStatus formats the status command output
func WriteGummyStatus(out io.Writer, info GummyStatusInfo) {
	w := newGummyWriter(out)

	// Determine state color and symbol
	stateColor := gummyFresh
	stateSymbol := gummyCheck
	isFresh := true
	switch info.State {
	case "fresh":
		stateColor = gummyFresh
		stateSymbol = gummyCheck
	case "stale":
		stateColor = gummyStale
		stateSymbol = "!"
		isFresh = false
	case "missing", "not_used":
		stateColor = gummyMissing
		stateSymbol = gummyCross
		isFresh = false
	}

	// Header line
	fmt.Fprintf(out, "snipe index: %s %s\n",
		stateColor.Sprint(info.State),
		stateColor.Sprint(stateSymbol))

	// If missing, show hint and return
	if info.State == "missing" {
		w.blank()
		fmt.Fprintf(out, "  %s Run: snipe index\n", gummyDim.Sprint(gummyArrow))
		return
	}

	w.blank()

	// Commit info
	if info.Commit != "" {
		commit := info.Commit
		if len(commit) > 7 {
			commit = commit[:7]
		}
		agePart := ""
		if info.AgeDesc != "" {
			agePart = " (" + info.AgeDesc + ")"
		}
		w.keyValue("Commit", gummyDim.Sprint(commit+agePart))
	}

	// Counts
	if info.Symbols > 0 {
		w.keyValueColored("Symbols", info.Symbols)
	}
	if info.Refs > 0 {
		w.keyValueColored("Refs", info.Refs)
	}
	if info.Calls > 0 {
		w.keyValueColored("Calls", info.Calls)
	}

	// Embeddings coverage
	if info.Symbols > 0 && info.Embedded > 0 {
		pct := float64(info.Embedded) / float64(info.Symbols) * 100
		coverage := fmt.Sprintf("%s / %s (%0.0f%%)",
			humanize.Comma(int64(info.Embedded)),
			humanize.Comma(int64(info.Symbols)),
			pct)
		w.keyValue("Embedded", colorByPct(coverage, pct))
	}

	// Enrichment coverage
	if info.Symbols > 0 && info.Enriched > 0 {
		pct := float64(info.Enriched) / float64(info.Symbols) * 100
		coverage := fmt.Sprintf("%s / %s (%0.0f%%)",
			humanize.Comma(int64(info.Enriched)),
			humanize.Comma(int64(info.Symbols)),
			pct)
		w.keyValue("Enriched", colorByPct(coverage, pct))
	}

	// Staleness warning
	if !isFresh {
		w.blank()
		fmt.Fprintf(out, "  %s Index may be outdated. Run: snipe index\n", gummyStale.Sprint("!"))
	}
}

// ============================================================================
// Helper functions
// ============================================================================

// colorByPct returns a coverage string colored by percentage threshold.
func colorByPct(s string, pct float64) string {
	switch {
	case pct >= 90:
		return gummyFresh.Sprint(s)
	case pct >= 50:
		return gummyStale.Sprint(s)
	default:
		return gummyDim.Sprint(s)
	}
}

// writeGummyError handles error display consistently
func writeGummyError(w *gummyWriter, err *Error) error {
	hint := ""
	if err.Next != nil {
		hint = err.Next.Command
	}
	w.errorWithHint(err.Message, hint)

	if len(err.Candidates) > 0 {
		w.candidates(err.Candidates)
	}
	return nil
}

// gummyWriteHints displays analysis hints
func gummyWriteHints(w *gummyWriter, hints []string, kgHints []KGHint) {
	if len(hints) == 0 && len(kgHints) == 0 {
		return
	}

	// Static hints
	for _, h := range hints {
		hintColor := gummyDim
		prefix := "hint"
		switch h {
		case HintDeprecated:
			hintColor = gummyStale
			prefix = "warning"
		case HintUnused:
			hintColor = gummyDim
		case HintPointerRecv:
			hintColor = gummyDim
		}
		fmt.Fprintf(w.out, "%s: %s\n", hintColor.Sprint(prefix), h)
	}

	// KG hints
	for _, h := range kgHints {
		hintColor := gummyDim
		prefix := h.Kind
		switch h.Severity {
		case "h":
			hintColor = gummyMissing
			prefix = "trap"
		case "m":
			hintColor = gummyStale
		}
		fmt.Fprintf(w.out, "%s: %s\n", hintColor.Sprint(prefix), h.Summary)
	}
}

// gummyFileGroup groups results by file
type gummyFileGroup struct {
	file    string
	results []Result
}

// gummyGroupByFile groups results by their file path
func gummyGroupByFile(results []Result) []gummyFileGroup {
	fileMap := make(map[string][]Result)
	var order []string

	for _, r := range results {
		if _, exists := fileMap[r.File]; !exists {
			order = append(order, r.File)
		}
		fileMap[r.File] = append(fileMap[r.File], r)
	}

	groups := make([]gummyFileGroup, 0, len(order))
	for _, file := range order {
		groups = append(groups, gummyFileGroup{
			file:    file,
			results: fileMap[file],
		})
	}

	return groups
}

// gummyCallerAggregate holds aggregated caller information
type gummyCallerAggregate struct {
	name  string
	file  string
	line  int
	count int
}

// gummyAggregateCallers groups results by caller function, counting call sites
func gummyAggregateCallers(results []Result) []gummyCallerAggregate {
	agg := make(map[string]*gummyCallerAggregate)
	var order []string

	for _, r := range results {
		key := r.Name + "|" + r.File
		if existing, ok := agg[key]; ok {
			existing.count++
		} else {
			order = append(order, key)
			agg[key] = &gummyCallerAggregate{
				name:  r.Name,
				file:  r.File,
				line:  r.Range.Start.Line,
				count: 1,
			}
		}
	}

	result := make([]gummyCallerAggregate, 0, len(order))
	for _, key := range order {
		result = append(result, *agg[key])
	}

	return result
}

// gummyCalleeAggregate holds aggregated callee information
type gummyCalleeAggregate struct {
	name     string
	file     string
	pkg      string
	lines    []int
	isStdlib bool
}

// gummyAggregateCallees groups results by callee function, collecting call lines
func gummyAggregateCallees(results []Result) []gummyCalleeAggregate {
	agg := make(map[string]*gummyCalleeAggregate)
	var order []string

	for _, r := range results {
		key := r.Name
		if existing, ok := agg[key]; ok {
			existing.lines = append(existing.lines, r.Range.Start.Line)
		} else {
			order = append(order, key)
			isStdlib := gummyIsStdlibPath(r.File, r.Package)
			agg[key] = &gummyCalleeAggregate{
				name:     r.Name,
				file:     r.File,
				pkg:      r.Package,
				lines:    []int{r.Range.Start.Line},
				isStdlib: isStdlib,
			}
		}
	}

	result := make([]gummyCalleeAggregate, 0, len(order))
	for _, key := range order {
		result = append(result, *agg[key])
	}

	return result
}

// gummyWriteCalleeItem formats a single callee entry
func gummyWriteCalleeItem(w *gummyWriter, c gummyCalleeAggregate) {
	nameWidth := 20
	namePart := c.name
	if len(namePart) > nameWidth {
		namePart = namePart[:nameWidth-3] + gummyEllipsis
	}

	// File or (stdlib) indicator
	filePart := c.file
	if c.isStdlib {
		filePart = "(stdlib)"
	} else if len(filePart) > 30 {
		parts := strings.Split(filePart, "/")
		if len(parts) > 2 {
			filePart = parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}

	// Line numbers
	linesPart := gummyFormatLineNumbers(c.lines)

	fmt.Fprintf(w.out, "  %-*s  %-30s  %s\n",
		nameWidth,
		namePart,
		gummyFile.Sprint(filePart),
		gummyDim.Sprint(linesPart))
}

// gummyFormatLineNumbers formats line numbers, truncating if too many
func gummyFormatLineNumbers(lines []int) string {
	if len(lines) == 0 {
		return ""
	}

	// Dedupe and sort
	unique := make(map[int]bool)
	for _, l := range lines {
		unique[l] = true
	}
	sorted := make([]int, 0, len(unique))
	for l := range unique {
		sorted = append(sorted, l)
	}
	sort.Ints(sorted)

	if len(sorted) == 1 {
		return fmt.Sprintf(":%d", sorted[0])
	}

	if len(sorted) <= 3 {
		parts := make([]string, len(sorted))
		for i, l := range sorted {
			parts[i] = fmt.Sprintf("%d", l)
		}
		return ":" + strings.Join(parts, ",")
	}

	return fmt.Sprintf(":%d,%d +%d", sorted[0], sorted[1], len(sorted)-2)
}

// gummyIsStdlibPath checks if a file/package is from the standard library
func gummyIsStdlibPath(file, pkg string) bool {
	if pkg != "" && !strings.Contains(pkg, ".") {
		return true
	}
	if strings.HasPrefix(file, "/usr/local/go") ||
		strings.HasPrefix(file, "/opt/go") ||
		strings.Contains(file, "/go/src/") {
		return true
	}
	return false
}
