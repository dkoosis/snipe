package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/protocol"
	"github.com/dkoosis/snipe/internal/telemetry"
)

// OutputFormat controls how the Writer renders responses.
type OutputFormat string

const (
	// OutputClaude renders terse, structured text optimized for Claude (default).
	OutputClaude OutputFormat = ""
	// OutputJSON renders the full JSON envelope (for toolchain integration).
	OutputJSON OutputFormat = "json"
)

// showSuggestionsEnabled controls whether suggestions are emitted in Claude text mode.
// On by default (AXI #9: contextual disclosure) — next-step command templates
// eliminate a round trip. Suppress with --no-suggestions (SetShowSuggestions).
var showSuggestionsEnabled = true

// SetShowSuggestions configures suggestion output for Claude mode.
func SetShowSuggestions(v bool) { showSuggestionsEnabled = v }

// processErrored records whether any emitted response signalled failure, so
// Execute can exit non-zero (AXI #6: agents gate on the exit code). A valid
// empty answer (Ok:true, zero results) leaves this false and keeps exit 0; an
// error envelope (WriteError, or a WriteResponse whose Ok is false) trips it.
var processErrored = false

// ProcessErrored reports whether an error envelope was emitted this run.
func ProcessErrored() bool { return processErrored }

// ResetProcessErrored clears the error flag; for in-process tests that reuse
// the package across cases.
func ResetProcessErrored() { processErrored = false }

// Writer handles output formatting for LLM consumers.
type Writer struct {
	out    io.Writer
	format OutputFormat
	start  time.Time
	// embedMissing, when set, appends `! noembed` to the meta line of Claude
	// output — an index-global self-assessment marker stamped once at store
	// open (see OpenStore) so every query command emits it uniformly.
	embedMissing bool
}

// SetEmbedMissing records that the open index holds no embeddings, so the
// `noembed` degraded marker should be emitted on this writer's responses.
func (w *Writer) SetEmbedMissing(missing bool) { w.embedMissing = missing }

// NewWriter creates a new output writer.
// format controls rendering: "" (default) = Claude-optimized text, "json" = full JSON envelope.
func NewWriter(out io.Writer, format OutputFormat) *Writer {
	return &Writer{
		out:    out,
		format: format,
		start:  time.Now(),
	}
}

// WriteResponse writes a response in the configured format.
func (w *Writer) WriteResponse(resp any) error {
	if m, ok := resp.(interface{ TelemetryCommand() string }); ok {
		f := telemetry.Fields{
			Command: m.TelemetryCommand(),
			Outcome: "ok",
			Ms:      time.Since(w.start).Milliseconds(),
		}
		// TelemetryMeta is the escape hatch Response[T] implements alongside
		// TelemetryCommand/IsOk (sn-r1do.1) — the Writer sees only `resp any`,
		// so it type-asserts for arg/rung/decision-path rather than knowing
		// every Response[T] instantiation.
		if tm, ok := resp.(interface {
			TelemetryMeta() (string, string, []string, string)
		}); ok {
			f.Arg, f.Rung, f.TriedRungs, f.IndexState = tm.TelemetryMeta()
		}
		telemetry.Emit(f)
	}
	if r, ok := resp.(interface{ IsOk() bool }); ok && !r.IsOk() {
		processErrored = true
	}
	switch w.format {
	case OutputJSON:
		return w.writeJSON(resp)
	case OutputHuman:
		return w.writeHuman(resp)
	case OutputClaude:
		return w.writeClaude(resp)
	}
	return w.writeClaude(resp)
}

// writeJSON writes the full JSON envelope (legacy format) as compact,
// single-line JSON — the shape toolchain subprocesses already consume.
func (w *Writer) writeJSON(resp any) error {
	return json.NewEncoder(w.out).Encode(resp)
}

// writeClaude renders a response as terse structured text optimized for Claude.
func (w *Writer) writeClaude(resp any) error {
	var b strings.Builder

	switch r := resp.(type) {
	case protocol.Response[protocol.Result]:
		w.writeClaudeResults(&b, r.Results, r.Meta, r.Suggestions, r.Error)
	case protocol.Response[protocol.Summary]:
		w.writeClaudeSummary(&b, r.Results, r.Meta)
	case protocol.Response[protocol.PackResult]:
		w.writeClaudePack(&b, r.Results, r.Meta)
	case protocol.Response[protocol.PackPackageResult]:
		w.writeClaudePackPackage(&b, r.Results, r.Meta)
	case protocol.Response[protocol.ExplainResult]:
		w.writeClaudeExplain(&b, r.Results, r.Meta)
	case protocol.Response[protocol.SymResult]:
		w.writeClaudeSym(&b, r.Results, r.Meta)
	case protocol.Response[protocol.DepsResult]:
		w.writeClaudeDeps(&b, r.Results, r.Meta)
	case protocol.Response[protocol.DepTreeResult]:
		w.writeClaudeDepTree(&b, r.Results, r.Meta)
	case protocol.Response[protocol.BoundaryResult]:
		w.writeClaudeBoundary(&b, r.Results, r.Meta)
	case protocol.Response[protocol.TypesResult]:
		w.writeClaudeTypes(&b, r.Results, r.Meta)
	case protocol.Response[protocol.LifecycleResult]:
		w.writeClaudeLifecycle(&b, r.Results, r.Meta)
	case protocol.Response[protocol.TraceResult]:
		w.writeClaudeTrace(&b, r.Results, r.Meta)
	default:
		// Fallback: JSON for unknown types
		return w.writeJSON(resp)
	}

	_, err := io.WriteString(w.out, b.String())
	return err
}

func (w *Writer) writeClaudeResults(b *strings.Builder, results []protocol.Result, meta protocol.Meta, suggestions []protocol.Suggestion, respErr *protocol.Error) {
	if respErr != nil {
		w.writeClaudeError(b, respErr)
		return
	}

	// Lead with a total (AXI #4: pre-computed aggregates eliminate a
	// header-counting round trip), matching diagram's "N packages · M edges"
	// convention.
	if meta.Total > 0 {
		if meta.Total == 1 {
			b.WriteString("1 result\n")
		} else {
			fmt.Fprintf(b, "%d results\n", meta.Total)
		}
	}

	for i := range results {
		r := &results[i]
		if i > 0 {
			b.WriteString("\n")
		}
		writeResultHeader(b, r)

		if r.Body != "" {
			b.WriteString("```go\n")
			b.WriteString(r.Body)
			b.WriteString("\n```\n")
		} else if r.Match != "" && (r.Kind == protocol.KindFunc || r.Kind == protocol.KindMethod) {
			// Signature line only for func/method — struct/type/const match is just the qualified name, redundant
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

func writeResultHeader(b *strings.Builder, r *protocol.Result) {
	// # Name [hex-id]
	b.WriteString("# ")
	if r.Receiver != "" {
		// Stored receivers already carry parens ("(*T)"); render a form
		// that pastes straight back into a query: (*T).Method.
		b.WriteString(parenRecv(r.Receiver))
		b.WriteString(".")
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

	// Doc comment (first sentence) — aids orientation without requiring pack
	if r.Doc != "" {
		b.WriteString("  ")
		b.WriteString(r.Doc)
		b.WriteString("\n")
	}

	// Role on its own line if present (set in detailed format)
	if r.Role != "" {
		b.WriteString("role: ")
		b.WriteString(r.Role)
		b.WriteString("\n")
	}

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

func (w *Writer) writeClaudeMeta(b *strings.Builder, meta protocol.Meta) {
	// Only include metadata that helps Claude, skip noise
	var parts []string
	if meta.Truncated {
		// Name the recovery lever so the agent can fetch the rest (AXI #3).
		// Most commands truncate at --limit; the token-budget path (no --limit)
		// falls through to a hint naming both possible levers.
		switch {
		case meta.Limit > 0 && meta.Total >= meta.Limit:
			parts = append(parts, fmt.Sprintf("truncated at --limit=%d (raise --limit or --offset %d for more)",
				meta.Limit, meta.Offset+meta.Total))
		default:
			parts = append(parts, "truncated (raise --max-tokens or --limit for the rest)")
		}
	}
	if len(meta.StaleFiles) > 0 {
		parts = append(parts, fmt.Sprintf("%d stale files", len(meta.StaleFiles)))
	}
	if meta.IndexState == protocol.IndexStale {
		parts = append(parts, "index stale")
	}
	if meta.IndexState == protocol.IndexMissing {
		parts = append(parts, "no index")
	}
	if len(meta.Degraded) > 0 {
		parts = append(parts, meta.Degraded...)
	}
	if w.embedMissing {
		parts = append(parts, protocol.DegradedNoEmbed)
	}
	for _, p := range parts {
		b.WriteString("! ")
		b.WriteString(p)
		b.WriteString("\n")
	}
}

func (w *Writer) writeClaudeError(b *strings.Builder, err *protocol.Error) {
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
				b.WriteString(parenRecv(c.Receiver))
				b.WriteString(".")
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

// parenRecv returns a receiver in re-typeable form: stored receivers already
// carry parens ("(*T)" or "(T)"); bare ones get wrapped.
func parenRecv(recv string) string {
	if strings.HasPrefix(recv, "(") {
		return recv
	}
	return "(" + recv + ")"
}

func writeClaudeSuggestions(b *strings.Builder, suggestions []protocol.Suggestion) {
	if !showSuggestionsEnabled || len(suggestions) == 0 {
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

func (w *Writer) writeClaudeSummary(b *strings.Builder, results []protocol.Summary, meta protocol.Meta) {
	for _, s := range results {
		fmt.Fprintf(b, "%d results", s.Total)
		if len(s.Kinds) > 0 {
			kinds := make([]string, 0, len(s.Kinds))
			for k := range s.Kinds {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			b.WriteString(" (")
			for i, k := range kinds {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(b, "%d %s", s.Kinds[k], k)
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

func (w *Writer) writeClaudePack(b *strings.Builder, results []protocol.PackResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
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

func (w *Writer) writeClaudePackPackage(b *strings.Builder, results []protocol.PackPackageResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
		fmt.Fprintf(b, "# package %s\n", r.Package)
		if r.Dir != "" {
			fmt.Fprintf(b, "dir: %s\n", r.Dir)
		}
		fmt.Fprintf(b, "files: %d  loc: %d  tests: %d  exports: %d\n",
			r.FileCount, r.LOC, r.TestCount, r.ExportCount)
		fmt.Fprintf(b, "imports: %d  dependents: %d\n", len(r.Imports), r.DependentCount)

		if len(r.KeyTypes) > 0 {
			b.WriteString("key_types:\n")
			for _, kt := range r.KeyTypes {
				fmt.Fprintf(b, "  %s %s\n", kt.Kind, kt.Name)
			}
		}

		if len(r.KeyFuncs) > 0 {
			b.WriteString("key_funcs:\n")
			for _, kf := range r.KeyFuncs {
				b.WriteString("  ")
				b.WriteString(kf.Name)
				if kf.Signature != "" {
					fmt.Fprintf(b, " — %s", kf.Signature)
				}
				b.WriteString("\n")
			}
		}

		if len(r.Imports) > 0 {
			b.WriteString("imports:\n")
			for _, dep := range r.Imports {
				fmt.Fprintf(b, "  %s", dep.Package)
				if dep.FileCount > 0 {
					fmt.Fprintf(b, " (%d files)", dep.FileCount)
				}
				b.WriteString("\n")
			}
		}

		if len(r.Files) > 0 {
			b.WriteString("files:\n")
			for _, fh := range r.Files {
				fmt.Fprintf(b, "  %s — %s\n", fh.Name, fh.Header)
			}
		}

		// Full export list omitted from Claude output — too noisy.
		// JSON consumers get it via the Exports field in the envelope.
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeExplain(b *strings.Builder, results []protocol.ExplainResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
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

func (w *Writer) writeClaudeSym(b *strings.Builder, results []protocol.SymResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
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
				for j := range r.Callers {
					c := &r.Callers[j]
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

func (w *Writer) writeClaudeDeps(b *strings.Builder, results []protocol.DepsResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
		fmt.Fprintf(b, "# %s\n", r.Package)
		if len(r.Dependencies) > 0 {
			fmt.Fprintf(b, "imports (%d):\n", len(r.Dependencies))
			for _, d := range r.Dependencies {
				fmt.Fprintf(b, "  %s (%d files)\n", d.Package, d.FileCount)
			}
		}
		if len(r.Dependents) > 0 {
			fmt.Fprintf(b, "imported by (%d):\n", len(r.Dependents))
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

func (w *Writer) writeClaudeTypes(b *strings.Builder, results []protocol.TypesResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
		fmt.Fprintf(b, "# %s\n", r.Symbol)
		fmt.Fprintf(b, "%s | %s", r.File, r.Kind)
		if r.Signature != "" {
			b.WriteString(" | ")
			b.WriteString(r.Signature)
		}
		b.WriteString("\n")
		if r.Doc != "" {
			b.WriteString(r.Doc)
			b.WriteString("\n")
		}
		if len(r.Fields) > 0 {
			b.WriteString("fields:\n")
			for _, f := range r.Fields {
				fmt.Fprintf(b, "  %s %s", f.Name, f.TypeExpr)
				if f.Tag != "" {
					fmt.Fprintf(b, " `%s`", f.Tag)
				}
				b.WriteString("\n")
			}
		}
		if len(r.Embeds) > 0 {
			b.WriteString("embeds:\n")
			for _, e := range r.Embeds {
				b.WriteString("  ")
				b.WriteString(e.TypeName)
				if e.FieldName != "" {
					fmt.Fprintf(b, " (as %s)", e.FieldName)
				}
				b.WriteString("\n")
			}
		}
		if len(r.Methods) > 0 {
			b.WriteString("methods:\n")
			for _, m := range r.Methods {
				fmt.Fprintf(b, "  %s", m.Name)
				if m.Signature != "" {
					b.WriteString(" — ")
					b.WriteString(m.Signature)
				}
				if m.File != "" {
					fmt.Fprintf(b, " (%s:%d)", m.File, m.Line)
				}
				b.WriteString("\n")
			}
		}
		if r.Implements.Status != "" && r.Implements.Status != "unknown" {
			fmt.Fprintf(b, "implements: %s", r.Implements.Status)
			if r.Implements.Note != "" {
				fmt.Fprintf(b, " (%s)", r.Implements.Note)
			}
			b.WriteString("\n")
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeLifecycle(b *strings.Builder, results []protocol.LifecycleResult, meta protocol.Meta) {
	for i := range results {
		r := &results[i]
		fmt.Fprintf(b, "# Lifecycle: %s", r.Type)
		if r.TypeID != "" {
			fmt.Fprintf(b, " [%s]", r.TypeID)
		}
		b.WriteString("\n")
		if r.TypeFile != "" {
			fmt.Fprintf(b, "%s:%d", r.TypeFile, r.TypeLine)
			if r.TypeKind != "" {
				fmt.Fprintf(b, " | %s", r.TypeKind)
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "%d refs across %d functions", r.TotalRefs, r.FunctionRefs)
		if r.TestRefs > 0 {
			fmt.Fprintf(b, " (+ %d in tests)", r.TestRefs)
		}
		b.WriteString("\n")

		if r.Summary {
			writeLifecycleSummary(b, *r)
			continue
		}

		for _, g := range r.Groups {
			// Standard CRUD buckets render even when empty so missing
			// classification is visible (snipe-0sb). Tests bucket is only
			// added by buildLifecycleResult when non-empty.
			if g.Count == 0 && !isStandardCRUDRole(g.Role) {
				continue
			}
			fmt.Fprintf(b, "\n## %s (%d)\n", g.Role, g.Count)
			for _, f := range g.Funcs {
				fmt.Fprintf(b, "- %s  %s:%d", f.Name, f.File, f.Line)
				if len(f.Mixed) > 0 {
					fmt.Fprintf(b, "  mixed:[%s]", strings.Join(f.Mixed, ","))
				}
				b.WriteString("\n")
				if f.Signal != "" {
					fmt.Fprintf(b, "    signal: %s\n", f.Signal)
				}
				if chain := lifecycleCallerChain(f.Callers); chain != "" {
					fmt.Fprintf(b, "    callers: %s\n", chain)
				}
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

// isStandardCRUDRole reports whether a group role is one of the canonical
// CRUD buckets that should render even when empty (snipe-0sb).
func isStandardCRUDRole(role string) bool {
	switch role {
	case "Create", "Mutate", "Read", "Delete", "Unknown":
		return true
	}
	return false
}

// writeLifecycleSummary renders a per-group one-line view: counts plus the
// function names only, no callers, no signal. Target: <50 lines for any type.
func writeLifecycleSummary(b *strings.Builder, r protocol.LifecycleResult) {
	for _, g := range r.Groups {
		if g.Count == 0 {
			if isStandardCRUDRole(g.Role) {
				fmt.Fprintf(b, "\n## %s (0)\n", g.Role)
			}
			continue
		}
		fmt.Fprintf(b, "\n## %s (%d)\n", g.Role, g.Count)
		const inlineCap = 12
		names := make([]string, 0, len(g.Funcs))
		for _, f := range g.Funcs {
			names = append(names, f.Name)
		}
		more := 0
		if len(names) > inlineCap {
			more = len(names) - inlineCap
			names = names[:inlineCap]
		}
		fmt.Fprintf(b, "%s", strings.Join(names, ", "))
		if more > 0 {
			fmt.Fprintf(b, ", +%d more", more)
		}
		b.WriteString("\n")
	}
}

// lifecycleCallerCap caps non-test caller names rendered inline.
// Test* / Benchmark* callers are folded into a count suffix so a hot symbol
// reachable from 80 tests does not blow the line / token budget (snipe-29j).
const lifecycleCallerCap = 8

// lifecycleCallerChain formats a BFS caller list as "fn ← caller1 ← caller2 ← ...".
// Depth-1 callers are listed in order; deeper hops follow sorted by depth then name.
// Test/Benchmark callers are collapsed into a "+Nt tests" suffix; remaining
// non-test callers are capped at lifecycleCallerCap with "+M more".
func lifecycleCallerChain(callers []protocol.LifecycleCallerNode) string {
	if len(callers) == 0 {
		return ""
	}
	sorted := make([]protocol.LifecycleCallerNode, len(callers))
	copy(sorted, callers)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && (sorted[j].Depth < sorted[j-1].Depth ||
			(sorted[j].Depth == sorted[j-1].Depth && sorted[j].Name < sorted[j-1].Name)); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	var nonTest []string
	tests := 0
	for _, c := range sorted {
		if isTestCaller(c.Name) {
			tests++
			continue
		}
		nonTest = append(nonTest, c.Name)
	}

	more := 0
	if len(nonTest) > lifecycleCallerCap {
		more = len(nonTest) - lifecycleCallerCap
		nonTest = nonTest[:lifecycleCallerCap]
	}

	parts := nonTest
	if more > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", more))
	}
	if tests > 0 {
		parts = append(parts, fmt.Sprintf("+%d tests", tests))
	}
	return strings.Join(parts, " ← ")
}

// isTestCaller reports whether a Go function name is a test or benchmark entry.
// Matches the testing package convention: Test*, Benchmark*, Fuzz*, Example*.
func isTestCaller(name string) bool {
	return strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Fuzz") ||
		strings.HasPrefix(name, "Example")
}

func (w *Writer) writeClaudeDepTree(b *strings.Builder, results []protocol.DepTreeResult, meta protocol.Meta) {
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

func (w *Writer) writeClaudeBoundary(b *strings.Builder, results []protocol.BoundaryResult, meta protocol.Meta) {
	for _, r := range results {
		fmt.Fprintf(b, "# boundary: {%s} ↔ {%s}\n",
			strings.Join(r.SetA, ","), strings.Join(r.SetB, ","))

		for _, dir := range r.Directions {
			fmt.Fprintf(b, "%s→%s: %d refs to %d symbols\n",
				dir.From, dir.To, dir.Total, len(dir.Symbols))
			for _, s := range dir.Symbols {
				fmt.Fprintf(b, "  %s.%s [%s] — %d refs\n",
					shortPkg(s.TargetPkg), s.Symbol, s.Kind, s.RefCount)
				for _, loc := range s.Locations {
					fmt.Fprintf(b, "    %s:%d\n", loc.File, loc.Line)
				}
			}
		}
	}
	w.writeClaudeMeta(b, meta)
}

func (w *Writer) writeClaudeTrace(b *strings.Builder, results []protocol.TraceResult, meta protocol.Meta) {
	if len(results) == 0 {
		w.writeClaudeError(b, &protocol.Error{Code: protocol.ErrNotFound, Message: "no string refs found"})
		return
	}

	value := results[0].Value
	fmt.Fprintf(b, "# %s — %d ref", value, meta.Total)
	if meta.Total != 1 {
		b.WriteString("s")
	}
	b.WriteString("\n\n")

	for i := range results {
		r := &results[i]
		// file:line | kind
		fmt.Fprintf(b, "%s:%d | %s\n", r.File, r.Line, r.Kind)

		// enclosing function + callers
		if r.Enclosing != nil {
			b.WriteString("  ∈ ")
			b.WriteString(r.Enclosing.Name)
			if len(r.Callers) > 0 {
				b.WriteString(" ← ")
				for i, c := range r.Callers {
					if i > 0 {
						b.WriteString(", ")
					}
					b.WriteString(c.Name)
					if i >= 2 && i < len(r.Callers)-1 {
						fmt.Fprintf(b, " +%d more", len(r.Callers)-i-1)
						break
					}
				}
			}
			b.WriteString("\n")
		}

		// snippet
		if r.Snippet != "" {
			b.WriteString("  ")
			b.WriteString(strings.TrimSpace(r.Snippet))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	w.writeClaudeMeta(b, meta)
}

// shortPkg returns the last path segment of a Go pkg_path for compact output.
func shortPkg(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// WriteError writes an error response. It carries no decision-path or index
// state — callers that have them in scope at the error site (def, sym, pack)
// call WriteErrorWithMeta instead. The query arg and alternative count are
// recovered from the error itself (NewNotFoundError / NewAmbiguousError
// remember them, sn-r1do.3), so usage.jsonl's NOT_FOUND/AMBIGUOUS_SYMBOL rows
// from refs, callers, callees, tests, impact, impl, explain, types and
// lifecycle are not arg-blind. This stays a thin wrapper so the dozens of call
// sites elsewhere keep compiling unchanged.
func (w *Writer) WriteError(command string, err *protocol.Error) error {
	query, hintCount := err.TelemetryArgs()
	return w.WriteErrorWithMeta(command, query, nil, "", hintCount, err)
}

// WriteErrorWithMeta writes an error response, enriching usage.jsonl with the
// query arg, the decision-path tried so far, the index state at error time,
// and candidateCount (sn-r1do.1). candidateCount is an explicit parameter
// rather than len(err.Candidates) because the "did you mean" suggestions on
// a NOT_FOUND error (NewNotFoundError's variadic strings) never populate
// Candidates — that field is AMBIGUOUS_SYMBOL-only ([]Candidate, full
// structs). Callers pass whichever count they actually have in scope; 0 when
// neither applies.
func (w *Writer) WriteErrorWithMeta(command, arg string, decisionPath []string, indexState protocol.IndexState, candidateCount int, err *protocol.Error) error {
	processErrored = true
	telemetry.Emit(telemetry.Fields{
		Command:        command,
		Outcome:        err.Code,
		Ms:             time.Since(w.start).Milliseconds(),
		Arg:            arg,
		Rung:           telemetry.ClassifyRung(decisionPath, nil, err.Code),
		CandidateCount: candidateCount,
		IndexState:     string(indexState),
		TriedRungs:     decisionPath,
	})
	if err.Next == nil {
		err.Next = protocol.DefaultNextForCode(err.Code)
	}
	if w.format == OutputHuman {
		var b strings.Builder
		writeHumanError(&b, err)
		_, writeErr := io.WriteString(w.out, b.String())
		return writeErr
	}
	if w.format != OutputJSON {
		var b strings.Builder
		w.writeClaudeError(&b, err)
		_, writeErr := io.WriteString(w.out, b.String())
		return writeErr
	}
	resp := protocol.Response[any]{
		Protocol: protocol.ProtocolVersion,
		Ok:       false,
		Results:  nil,
		Meta: protocol.Meta{
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

// WriteClaudePkgGrouped writes package symbols grouped by type: types with their
// methods and constructors clustered together, then standalone functions.
// Constants and vars are omitted — they rarely aid orientation.
func (w *Writer) WriteClaudePkgGrouped(results []protocol.Result, meta protocol.Meta) {
	var b strings.Builder

	// Partition by kind.
	typesByName := map[string]int{} // base name → index in types slice
	var types []protocol.Result
	var methods []protocol.Result
	var funcs []protocol.Result

	for i := range results {
		r := &results[i]
		switch r.Kind {
		case protocol.KindStruct, protocol.KindInterface, protocol.KindType:
			typesByName[r.Name] = len(types)
			types = append(types, *r)
		case protocol.KindMethod:
			methods = append(methods, *r)
		case protocol.KindFunc:
			funcs = append(funcs, *r)
			// const, var: omit
		}
	}

	// Index methods by receiver base type (strip pointer/parens).
	methodsByType := map[string][]protocol.Result{}
	var orphanMethods []protocol.Result
	for i := range methods {
		m := &methods[i]
		base := strings.TrimLeft(m.Receiver, "*()")
		base = strings.TrimRight(base, "()")
		if _, ok := typesByName[base]; ok {
			methodsByType[base] = append(methodsByType[base], *m)
		} else {
			orphanMethods = append(orphanMethods, *m)
		}
	}

	// Partition funcs: constructors (NewXxx where Xxx is a known type) vs standalone.
	constructorsByType := map[string][]protocol.Result{}
	var standaloneFuncs []protocol.Result
	for i := range funcs {
		f := &funcs[i]
		if strings.HasPrefix(f.Name, "New") {
			typeName := strings.TrimPrefix(f.Name, "New")
			if _, ok := typesByName[typeName]; ok {
				constructorsByType[typeName] = append(constructorsByType[typeName], *f)
				continue
			}
		}
		standaloneFuncs = append(standaloneFuncs, *f)
	}

	// Emit types with their constructors and methods.
	for ti := range types {
		t := &types[ti]
		if ti > 0 {
			b.WriteString("\n")
		}
		writeResultHeader(&b, t)
		ctors := constructorsByType[t.Name]
		for ci := range ctors {
			c := &ctors[ci]
			b.WriteString("\n")
			writeResultHeader(&b, c)
			if c.Match != "" {
				b.WriteString("  ")
				b.WriteString(c.Match)
				b.WriteString("\n")
			}
		}
		ms := methodsByType[t.Name]
		for mi := range ms {
			m := &ms[mi]
			b.WriteString("\n")
			writeResultHeader(&b, m)
			if m.Match != "" {
				b.WriteString("  ")
				b.WriteString(m.Match)
				b.WriteString("\n")
			}
		}
	}

	// Orphan methods (receiver type not exported from this package).
	for mi := range orphanMethods {
		m := &orphanMethods[mi]
		b.WriteString("\n")
		writeResultHeader(&b, m)
		if m.Match != "" {
			b.WriteString("  ")
			b.WriteString(m.Match)
			b.WriteString("\n")
		}
	}

	// Standalone functions — cap at 15 to avoid orientation noise.
	// Functions are pre-sorted by usage so the top ones are the entry points.
	const standaloneLimit = 15
	shown := standaloneFuncs
	var overflow []protocol.Result
	if len(standaloneFuncs) > standaloneLimit {
		shown = standaloneFuncs[:standaloneLimit]
		overflow = standaloneFuncs[standaloneLimit:]
	}
	for fi := range shown {
		f := &shown[fi]
		b.WriteString("\n")
		writeResultHeader(&b, f)
		if f.Match != "" {
			b.WriteString("  ")
			b.WriteString(f.Match)
			b.WriteString("\n")
		}
	}
	if len(overflow) > 0 {
		b.WriteString("\n")
		names := make([]string, len(overflow))
		for i := range overflow {
			names[i] = overflow[i].Name
		}
		fmt.Fprintf(&b, "...%d more: %s\n", len(overflow), strings.Join(names, ", "))
	}

	w.writeClaudeMeta(&b, meta)
	_, _ = io.WriteString(w.out, b.String())
}
