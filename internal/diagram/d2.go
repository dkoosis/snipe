// Package diagram emits D2 (https://d2lang.com) source from snipe graph data.
//
// The renderer is intentionally minimal: it knows how to escape labels, build
// containers (D2 nested objects), and write directed edges with optional
// styling. Callers shape the graph; this package only serializes.
//
// D1: diagrams are a side-channel for humans. The CLI commands wrapping this
// package print a Claude-optimized text summary first; the D2 source is
// additive.
package diagram

import (
	"fmt"
	"sort"
	"strings"
)

// Builder accumulates a D2 program. Zero value is ready to use.
//
// Containers are emitted as nested D2 objects keyed by their fully-qualified
// path ("cmd.lifecycle"). Nodes are emitted at the path supplied by the
// caller. Edges are flattened to the top level using fully-qualified ids.
type Builder struct {
	// Title is rendered as a top-level comment (D2 has no native title).
	Title string
	// Direction is the D2 layout direction: "right", "down", "left", "up".
	// Empty falls back to D2's default ("down").
	Direction string

	containers map[string]*container // path -> container metadata
	nodes      []node
	edges      []edge
}

type container struct {
	Path  string
	Label string
	Style map[string]string
}

type node struct {
	Path    string // dot-separated container path; final segment is the node id
	Label   string
	Shape   string
	Style   map[string]string
	Members []string // shape:class body lines (fields/methods), quoted verbatim at render time
}

type edge struct {
	From   string
	To     string
	Label  string
	Style  map[string]string
	Dashed bool
}

// AddContainer registers a container. Calling twice with the same path is a
// no-op; the first label/style wins.
func (b *Builder) AddContainer(path, label string, style map[string]string) {
	if path == "" {
		return
	}
	if b.containers == nil {
		b.containers = make(map[string]*container)
	}
	if _, ok := b.containers[path]; ok {
		return
	}
	b.containers[path] = &container{Path: path, Label: label, Style: style}
}

// AddNode adds a leaf node at the given path. The path may include container
// segments separated by dots; intermediate containers must already be
// registered or will be auto-created with a label equal to the segment.
func (b *Builder) AddNode(path, label, shape string, style map[string]string) {
	if path == "" {
		return
	}
	b.ensureParents(path)
	b.nodes = append(b.nodes, node{Path: path, Label: label, Shape: shape, Style: style})
}

// AddEdge adds a directed edge between two fully-qualified node paths.
func (b *Builder) AddEdge(from, to, label string, style map[string]string) {
	if from == "" || to == "" {
		return
	}
	b.edges = append(b.edges, edge{From: from, To: to, Label: label, Style: style})
}

// AddDashedEdge is a convenience that emits a dashed edge.
func (b *Builder) AddDashedEdge(from, to, label string) {
	b.edges = append(b.edges, edge{From: from, To: to, Label: label, Dashed: true})
}

// AddClassNode adds a shape:class node whose body lists members (fields and
// methods), one per line. Each member is written as its own quoted D2 string
// rather than bare class-member syntax ("name: type") because arbitrary Go
// type text — map[string]int, *sync.Mutex, generic constraints — breaks D2's
// bare-member grammar (brackets and colons collide with its parser); a quoted
// line survives untouched while D2 still renders it as a class member,
// including a leading "+"/"-" visibility glyph when the caller includes one.
func (b *Builder) AddClassNode(path, label string, members []string, style map[string]string) {
	if path == "" {
		return
	}
	b.ensureParents(path)
	b.nodes = append(b.nodes, node{Path: path, Label: label, Shape: "class", Style: style, Members: members})
}

func (b *Builder) ensureParents(path string) {
	parts := strings.Split(path, ".")
	if len(parts) <= 1 {
		return
	}
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[:i], ".")
		b.AddContainer(parent, parts[i-1], nil)
	}
}

// Render returns the D2 source as a string.
func (b *Builder) Render() string {
	var w strings.Builder
	if b.Title != "" {
		fmt.Fprintf(&w, "# %s\n", b.Title)
	}
	if b.Direction != "" {
		fmt.Fprintf(&w, "direction: %s\n", b.Direction)
	}
	if b.Title != "" || b.Direction != "" {
		w.WriteByte('\n')
	}

	// Emit containers in deterministic order so output is stable.
	paths := make([]string, 0, len(b.containers))
	for p := range b.containers {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		c := b.containers[p]
		writeContainer(&w, c)
	}

	// Emit nodes in insertion order — callers control ordering.
	for _, n := range b.nodes {
		writeNode(&w, n)
	}

	// Emit edges last.
	for _, e := range b.edges {
		writeEdge(&w, e)
	}

	return w.String()
}

func writeContainer(w *strings.Builder, c *container) {
	fmt.Fprintf(w, "%s: %s {\n", c.Path, quote(c.Label))
	for _, kv := range sortedStyle(c.Style) {
		fmt.Fprintf(w, "  style.%s: %s\n", kv[0], styleValue(kv[1]))
	}
	w.WriteString("}\n")
}

func writeNode(w *strings.Builder, n node) {
	if n.Shape == "" && len(n.Style) == 0 && len(n.Members) == 0 {
		fmt.Fprintf(w, "%s: %s\n", n.Path, quote(n.Label))
		return
	}
	fmt.Fprintf(w, "%s: %s {\n", n.Path, quote(n.Label))
	if n.Shape != "" {
		fmt.Fprintf(w, "  shape: %s\n", n.Shape)
	}
	for _, kv := range sortedStyle(n.Style) {
		fmt.Fprintf(w, "  style.%s: %s\n", kv[0], styleValue(kv[1]))
	}
	for _, m := range n.Members {
		fmt.Fprintf(w, "  %s\n", quote(m))
	}
	w.WriteString("}\n")
}

func writeEdge(w *strings.Builder, e edge) {
	prefix := fmt.Sprintf("%s -> %s", e.From, e.To)
	hasAttrs := e.Label != "" || e.Dashed || len(e.Style) > 0
	if !hasAttrs {
		fmt.Fprintf(w, "%s\n", prefix)
		return
	}
	if e.Label != "" && !e.Dashed && len(e.Style) == 0 {
		fmt.Fprintf(w, "%s: %s\n", prefix, quote(e.Label))
		return
	}
	if e.Label != "" {
		fmt.Fprintf(w, "%s: %s {\n", prefix, quote(e.Label))
	} else {
		fmt.Fprintf(w, "%s: {\n", prefix)
	}
	if e.Dashed {
		w.WriteString("  style.stroke-dash: 3\n")
	}
	for _, kv := range sortedStyle(e.Style) {
		fmt.Fprintf(w, "  style.%s: %s\n", kv[0], styleValue(kv[1]))
	}
	w.WriteString("}\n")
}

// styleValue quotes a style value when D2 would otherwise misparse it
// (the most common case: hex colors starting with '#', which D2 treats as
// comments). Bare booleans and bare keywords are passed through.
func styleValue(v string) string {
	if v == "" {
		return `""`
	}
	if v == "true" || v == "false" {
		return v
	}
	// Hex color or anything starting with '#' must be quoted.
	if v[0] == '#' {
		return quote(v)
	}
	// Quote anything containing whitespace or special chars.
	for _, r := range v {
		if r == ' ' || r == '\t' || r == '"' || r == '\n' || r == ':' {
			return quote(v)
		}
	}
	return v
}

func sortedStyle(m map[string]string) [][2]string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([][2]string, len(keys))
	for i, k := range keys {
		out[i] = [2]string{k, m[k]}
	}
	return out
}

// quote wraps s in double quotes, escaping internal quotes and backslashes.
// Used for D2 string literals that may contain special characters
// (slashes, dots in package paths, etc.).
func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\n")
		case '\r':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// SanitizeID converts an arbitrary string into a D2-safe identifier segment.
// D2 identifiers may not contain dots (path separator), spaces, or quotes.
// Replacement is deterministic so the same input maps to the same id across
// runs — important for stable diagram output.
func SanitizeID(raw string) string {
	if raw == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch r {
		case '.', '/', '\\', ' ', '"', '\'', '`', '{', '}', '(', ')':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
