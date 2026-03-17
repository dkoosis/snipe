package output

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/util"
)

// globalFileCache is the default file cache for output operations.
var globalFileCache = util.NewFileCache(util.DefaultMaxCachedFiles)

// OutputFormat controls how the Writer renders responses.
type OutputFormat string

const (
	// OutputClaude renders terse, structured text optimized for Claude (default).
	OutputClaude OutputFormat = ""
	// OutputJSON renders the full JSON envelope (for orca/toolchain integration).
	OutputJSON OutputFormat = "json"
)

// Writer handles output formatting for LLM consumers.
type Writer struct {
	out     io.Writer
	compact bool
	format  OutputFormat
	start   time.Time
}

// NewWriter creates a new output writer.
// format controls rendering: "" (default) = Claude-optimized text, "json" = full JSON envelope.
func NewWriter(out io.Writer, compact bool, format OutputFormat) *Writer {
	return &Writer{
		out:     out,
		compact: compact,
		format:  format,
		start:   time.Now(),
	}
}

// WriteResponse writes a response in the configured format.
func (w *Writer) WriteResponse(resp any) error {
	if w.format == OutputJSON {
		return w.writeJSON(resp)
	}
	return w.writeClaude(resp)
}

// writeJSON writes the full JSON envelope (legacy/orca format).
func (w *Writer) writeJSON(resp any) error {
	enc := json.NewEncoder(w.out)
	if !w.compact {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(resp)
}

// writeClaude renders a response as terse structured text optimized for Claude.
func (w *Writer) writeClaude(resp any) error {
	var b strings.Builder

	switch r := resp.(type) {
	case Response[Result]:
		w.writeClaudeResults(&b, r.Results, r.Meta, r.Suggestions, r.Error)
	case Response[Summary]:
		w.writeClaudeSummary(&b, r.Results, r.Meta)
	case Response[PackResult]:
		w.writeClaudePack(&b, r.Results, r.Meta)
	case Response[ExplainResult]:
		w.writeClaudeExplain(&b, r.Results, r.Meta)
	case Response[SymResult]:
		w.writeClaudeSym(&b, r.Results, r.Meta)
	case Response[DepsResult]:
		w.writeClaudeDeps(&b, r.Results, r.Meta)
	case Response[DepTreeResult]:
		w.writeClaudeDepTree(&b, r.Results, r.Meta)
	default:
		// Fallback: JSON for unknown types
		return w.writeJSON(resp)
	}

	_, err := io.WriteString(w.out, b.String())
	return err
}

func (w *Writer) writeClaudeResults(b *strings.Builder, results []Result, meta Meta, suggestions []Suggestion, respErr *Error) {
	if respErr != nil {
		w.writeClaudeError(b, respErr)
		return
	}

	for i, r := range results {
		if i > 0 {
			b.WriteString("\n")
		}
		writeResultHeader(b, &r)

		if r.Body != "" {
			b.WriteString("```go\n")
			b.WriteString(r.Body)
			b.WriteString("\n```\n")
		} else if r.Match != "" && r.Kind != "" {
			// Signature line only (no body)
			b.WriteString("  ")
			b.WriteString(r.Match)
			b.WriteString("\n")
		}
	}

	if meta.Total == 0 && respErr == nil {
		b.WriteString("No results.\n")
	}

	w.writeClaudeMeta(b, meta)
	writeClaudeSuggestions(b, suggestions)
}

func writeResultHeader(b *strings.Builder, r *Result) {
	// # Name [hex-id]
	b.WriteString("# ")
	if r.Receiver != "" {
		b.WriteString("(")
		b.WriteString(r.Receiver)
		b.WriteString(").")
	}
	b.WriteString(r.Name)
	if r.ID != "" {
		b.WriteString(" [")
		b.WriteString(r.ID)
		b.WriteString("]")
	}
	b.WriteString("\n")

	// file:line-line | kind | refs | callers
	b.WriteString(r.File)
	if r.Range.Start.Line > 0 {
		fmt.Fprintf(b, ":%d", r.Range.Start.Line)
		if r.Range.End.Line > r.Range.Start.Line {
			fmt.Fprintf(b, "-%d", r.Range.End.Line)
		}
	}
	if r.Kind != "" {
		b.WriteString(" | ")
		b.WriteString(r.Kind)
	}
	if r.RefCount > 0 {
		fmt.Fprintf(b, " | %d refs", r.RefCount)
	}
	if len(r.CallersPreview) > 0 {
		b.WriteString(" | callers: ")
		for j, cp := range r.CallersPreview {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(cp.Name)
			if j >= 2 && j < len(r.CallersPreview)-1 {
				fmt.Fprintf(b, " +%d more", len(r.CallersPreview)-j-1)
				break
			}
		}
	}
	b.WriteString("\n")

	// Hints on their own line if present
	if len(r.Hints) > 0 {
		b.WriteString("hints: ")
		for j, h := range r.Hints {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(h)
		}
		b.WriteString("\n")
	}
}

func (w *Writer) writeClaudeMeta(b *strings.Builder, meta Meta) {
	// Only include metadata that helps Claude, skip noise
	var parts []string
	if meta.Truncated {
		parts = append(parts, "truncated")
	}
	if len(meta.StaleFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%d stale files", len(meta.StaleFiles)))
	}
	if meta.IndexState == IndexStale {
		parts = append(parts, "index stale")
	}
	if meta.IndexState == IndexMissing {
		parts = append(parts, "no index")
	}
	if len(meta.Degraded) > 0 {
		parts = append(parts, meta.Degraded...)
	}
	if len(parts) > 0 {
		b.WriteString("---\n")
		for _, p := range parts {
			b.WriteString("! ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}
}

func (w *Writer) writeClaudeError(b *strings.Builder, err *Error) {
	b.WriteString("error: ")
	b.WriteString(err.Message)
	b.WriteString("\n")

	if err.Next != nil {
		b.WriteString("next: ")
		b.WriteString(err.Next.Command)
		b.WriteString("\n")
	}

	if len(err.Candidates) > 0 {
		b.WriteString("candidates:\n")
		for _, c := range err.Candidates {
			b.WriteString("  ")
			if c.Receiver != "" {
				b.WriteString("(")
				b.WriteString(c.Receiver)
				b.WriteString(").")
			}
			b.WriteString(c.Name)
			b.WriteString(" [")
			b.WriteString(c.ID)
			b.WriteString("] ")
			b.WriteString(c.File)
			b.WriteString(" | ")
			b.WriteString(c.Kind)
			b.WriteString("\n")
		}
	}

	writeClaudeSuggestions(b, err.Suggestions)
}

func writeClaudeSuggestions(b *strings.Builder, suggestions []Suggestion) {
	if len(suggestions) == 0 {
		return
	}
	for _, s := range suggestions {
		if s.Command != "" {
			b.WriteString("? ")
			b.WriteString(s.Command)
			if s.Description != "" {
				b.WriteString("  -- ")
				b.WriteString(s.Description)
			}
			b.WriteString("\n")
		}
	}
}

func (w *Writer) writeClaudeSummary(b *strings.Builder, results []Summary, meta Meta) {
	for _, s := range results {
		fmt.Fprintf(b, "%d results", s.Total)
		if len(s.Kinds) > 0 {
			b.WriteString(" (")
			first := true
			for k, v := range s.Kinds {
				if !first {
					b.WriteString(", ")
				}
				fmt.Fprintf(b, "%d %s", v, k)
				first = false
			}
			b.WriteString(")")
		}
		b.WriteString("\n")
		for _, f := range s.Files {
			fmt.Fprintf(b, "  %s: %d\n", f.File, f.Count)
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudePack(b *strings.Builder, results []PackResult, meta Meta) {
	for _, r := range results {
		if r.Definition != nil {
			writeResultHeader(b, r.Definition)
			if r.Purpose != "" {
				b.WriteString("purpose: ")
				b.WriteString(r.Purpose)
				b.WriteString("\n")
			}
			if r.Role != "" {
				b.WriteString("role: ")
				b.WriteString(r.Role)
				b.WriteString("\n")
			}
			if r.Definition.Body != "" {
				b.WriteString("```go\n")
				b.WriteString(r.Definition.Body)
				b.WriteString("\n```\n")
			}
		}
		if len(r.Methods) > 0 {
			b.WriteString("methods:\n")
			for _, m := range r.Methods {
				b.WriteString("  ")
				b.WriteString(m.Name)
				if m.Signature != "" {
					b.WriteString(" — ")
					b.WriteString(m.Signature)
				}
				b.WriteString("\n")
			}
		}
		if r.CallerCount > 0 {
			fmt.Fprintf(b, "%d callers", r.CallerCount)
			if r.RefCount > 0 {
				fmt.Fprintf(b, ", %d refs", r.RefCount)
			}
			b.WriteString("\n")
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeExplain(b *strings.Builder, results []ExplainResult, meta Meta) {
	for _, r := range results {
		fmt.Fprintf(b, "# %s\n", r.Symbol)
		fmt.Fprintf(b, "%s | %s\n", r.File, r.Kind)
		if r.Signature != "" {
			b.WriteString("  ")
			b.WriteString(r.Signature)
			b.WriteString("\n")
		}
		if r.Purpose != "" {
			b.WriteString("purpose: ")
			b.WriteString(r.Purpose)
			b.WriteString("\n")
		}
		if len(r.Mechanism) > 0 {
			b.WriteString("mechanism:\n")
			for _, step := range r.Mechanism {
				fmt.Fprintf(b, "  %s %s", step.Action, step.Target)
				if step.Note != "" {
					fmt.Fprintf(b, " (%s)", step.Note)
				}
				b.WriteString("\n")
			}
		}
		if len(r.Warnings) > 0 {
			for _, warn := range r.Warnings {
				fmt.Fprintf(b, "! %s L%d: %s\n", warn.Severity, warn.Line, warn.Message)
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeSym(b *strings.Builder, results []SymResult, meta Meta) {
	for _, r := range results {
		if r.Definition != nil {
			writeResultHeader(b, r.Definition)
			if r.Definition.Body != "" {
				b.WriteString("```go\n")
				b.WriteString(r.Definition.Body)
				b.WriteString("\n```\n")
			}
		}
		if r.CallerCount > 0 {
			fmt.Fprintf(b, "%d callers", r.CallerCount)
			if len(r.Callers) > 0 {
				b.WriteString(": ")
				for j, c := range r.Callers {
					if j > 0 {
						b.WriteString(", ")
					}
					b.WriteString(c.Name)
					if j >= 4 {
						fmt.Fprintf(b, " +%d more", r.CallerCount-j-1)
						break
					}
				}
			}
			b.WriteString("\n")
		}
		if r.CalleeCount > 0 {
			fmt.Fprintf(b, "%d callees\n", r.CalleeCount)
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeDeps(b *strings.Builder, results []DepsResult, meta Meta) {
	for _, r := range results {
		fmt.Fprintf(b, "# %s\n", r.Package)
		if len(r.Dependencies) > 0 {
			b.WriteString("imports:\n")
			for _, d := range r.Dependencies {
				fmt.Fprintf(b, "  %s (%d files)\n", d.Package, d.FileCount)
			}
		}
		if len(r.Dependents) > 0 {
			b.WriteString("imported by:\n")
			for _, d := range r.Dependents {
				fmt.Fprintf(b, "  %s (%d files)\n", d.Package, d.FileCount)
			}
		}
		if len(r.Cycles) > 0 {
			b.WriteString("! cycles:\n")
			for _, c := range r.Cycles {
				fmt.Fprintf(b, "  %s\n", strings.Join(c, " -> "))
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeDepTree(b *strings.Builder, results []DepTreeResult, meta Meta) {
	for _, r := range results {
		fmt.Fprintf(b, "%d packages, %d edges\n", len(r.Packages), len(r.Edges))
		for _, e := range r.Edges {
			fmt.Fprintf(b, "  %s -> %s (%d files)\n", e.From, e.To, e.FileCount)
		}
		if len(r.Cycles) > 0 {
			b.WriteString("! cycles:\n")
			for _, c := range r.Cycles {
				fmt.Fprintf(b, "  %s\n", strings.Join(c, " -> "))
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

// WriteError writes an error response
func (w *Writer) WriteError(command string, err *Error) error {
	if w.format != OutputJSON {
		var b strings.Builder
		w.writeClaudeError(&b, err)
		_, writeErr := io.WriteString(w.out, b.String())
		return writeErr
	}
	resp := Response[any]{
		Protocol: ProtocolVersion,
		Ok:       false,
		Results:  nil,
		Meta: Meta{
			Command: command,
			Ms:      time.Since(w.start).Milliseconds(),
		},
		Error:       err,
		Suggestions: err.Suggestions,
	}
	return w.writeJSON(resp)
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

// EstimateResultTokens estimates token count for a single result.
func EstimateResultTokens(r *Result) int {
	tokens := EstimateTokens(r.Name)
	tokens += EstimateTokens(r.File)
	tokens += EstimateTokens(r.Match)
	if r.Body != "" {
		tokens += EstimateTokens(r.Body)
	}
	if r.Context != nil {
		for _, line := range r.Context.Before {
			tokens += EstimateTokens(line)
		}
		for _, line := range r.Context.After {
			tokens += EstimateTokens(line)
		}
	}
	if r.Enclosing != nil {
		tokens += EstimateTokens(r.Enclosing.Signature)
	}
	// Add overhead for JSON structure (~50 tokens per result)
	tokens += 50
	return tokens
}

// TruncateToTokenBudget truncates results to fit within a token budget.
// Returns the truncated slice and whether truncation occurred.
// If maxTokens is 0, returns the original slice unchanged.
func TruncateToTokenBudget(results []Result, maxTokens int) ([]Result, bool) {
	if maxTokens <= 0 {
		return results, false
	}

	// Reserve tokens for response wrapper (meta, error fields, etc.)
	const overhead = 200
	budget := maxTokens - overhead
	if budget <= 0 {
		return nil, len(results) > 0
	}

	var truncated []Result
	totalTokens := 0

	for i := range results {
		resultTokens := EstimateResultTokens(&results[i])
		if totalTokens+resultTokens > budget && len(truncated) > 0 {
			// Would exceed budget and we have at least one result
			return truncated, true
		}
		totalTokens += resultTokens
		truncated = append(truncated, results[i])
	}

	return truncated, false
}

// formatEditTarget formats a range as an edit target string.
// If hash is non-empty, appends it for change detection: file:L:C-L:C@hash
func formatEditTarget(file string, r Range, hash string) string {
	target := fmt.Sprintf("%s:%d:%d-%d:%d",
		file,
		r.Start.Line, r.Start.Col,
		r.End.Line, r.End.Col,
	)
	if hash != "" {
		target += "@" + hash
	}
	return target
}

// computeRangeHash computes a SHA256 hash of the content within a line range.
// Returns a truncated hash (16 hex chars) for embedding in edit_target.
// If the range cannot be read, returns an empty string.
func computeRangeHash(file string, r Range) string {
	lines, err := readFileLines(file)
	if err != nil {
		return ""
	}

	startLine := r.Start.Line
	endLine := r.End.Line

	// Validate range
	if startLine < 1 || endLine < startLine || startLine > len(lines) {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}

	// Extract lines in range
	var content strings.Builder
	for i := startLine; i <= endLine; i++ {
		if i > startLine {
			content.WriteString("\n")
		}
		content.WriteString(lines[i-1])
	}

	// Compute SHA256 and truncate to 16 hex chars (8 bytes)
	h := sha256.Sum256([]byte(content.String()))
	return hex.EncodeToString(h[:8])
}

// FormatEditTargetWithHash is a convenience function that computes the range hash
// and formats the edit target in one call.
// fileRel is the relative path (for output), fileAbs is the absolute path (for reading file to compute hash).
func FormatEditTargetWithHash(fileRel, fileAbs string, r Range) string {
	hash := computeRangeHash(fileAbs, r)
	return formatEditTarget(fileRel, r, hash)
}

// AddContext loads N lines of context before and after the result's range
func AddContext(result *Result, n int) error {
	if n <= 0 {
		return nil
	}

	// Use absolute path for file operations
	filePath := result.FileAbs
	if filePath == "" {
		filePath = result.File // Fallback if FileAbs not set
	}
	lines, err := readFileLines(filePath)
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

// ScoreResult calculates a relevance score for a result based on match quality.
// Higher scores indicate better matches. Scoring factors:
// - Exact name match: +100
// - Prefix match: +50
// - Definition (vs reference): +30
// - Public symbol (uppercase): +20
// - Shorter file path: +10 (normalized)
func ScoreResult(result *Result, query string) float64 {
	var score float64

	name := result.Name
	queryLower := strings.ToLower(query)
	nameLower := strings.ToLower(name)

	// Match scoring (case-insensitive)
	switch {
	case nameLower == queryLower:
		score += 100 // Exact match
	case strings.HasPrefix(nameLower, queryLower):
		score += 50 // Prefix match
	case strings.Contains(nameLower, queryLower):
		score += 25 // Contains match
	}

	// Bonus for definitions over references
	switch result.Kind {
	case "func", "method", "type", "struct", "interface", "const", "var":
		score += 30
	}

	// Bonus for exported/public symbols
	if len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		score += 20
	}

	// Slight bonus for shorter paths (more likely to be core code)
	pathLen := len(result.File)
	if pathLen > 0 {
		score += 10.0 * (1.0 - float64(min(pathLen, 100))/100.0)
	}

	return score
}

// scoreResults applies relevance scoring to all results.
func scoreResults(results []Result, query string) {
	for i := range results {
		results[i].Score = ScoreResult(&results[i], query)
	}
}

// sortByScore sorts results by score in descending order (highest first).
// Uses stable sort with deterministic tie-breaking by File, then Name.
func sortByScore(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].File != results[j].File {
			return results[i].File < results[j].File
		}
		return results[i].Name < results[j].Name
	})
}

// ScoreAndSort scores results by relevance and sorts by score descending.
func ScoreAndSort(results []Result, query string) {
	scoreResults(results, query)
	sortByScore(results)
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
	// Use absolute path for file operations
	filePath := result.FileAbs
	if filePath == "" {
		filePath = result.File // Fallback if FileAbs not set
	}
	lines, err := readFileLines(filePath)
	if err != nil {
		return err
	}

	startLine := result.Range.Start.Line
	endLine := result.Range.End.Line

	if startLine < 1 || endLine > len(lines) {
		return nil // Invalid range, skip
	}

	// Extract lines from startLine to endLine (1-indexed)
	result.Body = strings.Join(lines[startLine-1:endLine], "\n")
	return nil
}

// truncateBodySemantic truncates the Body field at a semantic boundary
// (complete statement or declaration) to fit within maxLines.
// Returns true if truncation occurred.
func truncateBodySemantic(result *Result, maxLines int) bool {
	if result.Body == "" || maxLines <= 0 {
		return false
	}

	lines := strings.Split(result.Body, "\n")
	if len(lines) <= maxLines {
		return false
	}

	// Find the best truncation point at a statement boundary
	truncateAt := findSemanticBoundary(lines, maxLines)
	if truncateAt <= 0 {
		truncateAt = maxLines // Fallback to hard limit
	}

	result.Body = strings.Join(lines[:truncateAt], "\n") + "\n// ... truncated"
	return true
}

// findSemanticBoundary finds the best line to truncate at, looking for
// statement boundaries (lines ending with ; or } or { at appropriate nesting).
// Returns 0 if no good boundary found before maxLines.
func findSemanticBoundary(lines []string, maxLines int) int {
	bestLine := 0
	braceDepth := 0

	for i := 0; i < maxLines && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		// Track brace depth
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")

		// Good truncation points: complete statements at depth 0 or 1
		if braceDepth <= 1 {
			// Line ends with semicolon (statement end)
			if strings.HasSuffix(line, ";") {
				bestLine = i + 1
			}
			// Line ends with closing brace (block end)
			if strings.HasSuffix(line, "}") {
				bestLine = i + 1
			}
			// Line ends with opening brace (start of block - include it)
			if strings.HasSuffix(line, "{") && i < maxLines-1 {
				bestLine = i + 1
			}
		}
	}

	return bestLine
}

// truncateResultsSemantic truncates results to fit within a token budget,
// preferring to keep complete results and truncating bodies at semantic boundaries.
// Returns the truncated slice, whether truncation occurred, and the estimated token count.
func truncateResultsSemantic(results []Result, maxTokens int, maxBodyLines int) ([]Result, bool, int) {
	if maxTokens <= 0 {
		total := 0
		for i := range results {
			total += EstimateResultTokens(&results[i])
		}
		return results, false, total
	}

	// Reserve tokens for response wrapper
	const overhead = 200
	budget := maxTokens - overhead
	if budget <= 0 {
		return nil, len(results) > 0, 0
	}

	var truncated []Result
	totalTokens := 0
	didTruncate := false

	for i := range results {
		result := results[i] // Copy to allow modification

		// First, try to fit the full result
		resultTokens := EstimateResultTokens(&result)

		if totalTokens+resultTokens <= budget {
			totalTokens += resultTokens
			truncated = append(truncated, result)
			continue
		}

		// Result doesn't fit - try truncating its body
		if result.Body != "" && maxBodyLines > 0 {
			if truncateBodySemantic(&result, maxBodyLines) {
				didTruncate = true
				resultTokens = EstimateResultTokens(&result)
				if totalTokens+resultTokens <= budget {
					totalTokens += resultTokens
					truncated = append(truncated, result)
					continue
				}
			}
		}

		// Still doesn't fit - stop adding results
		if len(truncated) > 0 {
			didTruncate = true
			break
		}

		// First result must be included even if over budget
		totalTokens += resultTokens
		truncated = append(truncated, result)
		didTruncate = true
		break
	}

	return truncated, didTruncate, totalTokens
}
