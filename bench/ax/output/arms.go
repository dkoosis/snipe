package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// listing is one captured snipe result list, parsed from today's text. Every
// arm renders from it, so every arm carries the same facts.
type listing struct {
	Head  []string `json:"notes_head,omitempty"`
	Items []item   `json:"items"`
	Tail  []string `json:"notes_tail,omitempty"`
}

// item is one result: the header line, the location line and what follows it.
type item struct {
	Section []string `json:"section,omitempty"` // notes printed just before this item
	Name    string   `json:"name"`
	ID      string   `json:"id"`
	Loc     string   `json:"loc"`
	Kind    string   `json:"kind"`
	Extra   []string `json:"extra,omitempty"`
	Hints   string   `json:"hints,omitempty"`
	Sig     []string `json:"detail,omitempty"`
}

var (
	itemHead = regexp.MustCompile(`^# (.+) \[([0-9a-f]{16})\]$`)
	locLine  = regexp.MustCompile(`^(\S+:\d+(?:-\d+)?) \| (.+)$`)
)

// parseText reads snipe's concise text. A line it cannot place in an item is a
// note: before the first item it heads the list, between items it opens the
// next item's section, and after the last item it tails the list.
func parseText(s string) (listing, error) {
	var l listing
	var cur *item
	var pending []string
	wantLoc := false
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		switch m := itemHead.FindStringSubmatch(line); {
		case m != nil:
			l.Items = append(l.Items, item{Section: pending, Name: m[1], ID: m[2]})
			pending = nil
			cur, wantLoc = &l.Items[len(l.Items)-1], true
		case wantLoc:
			lm := locLine.FindStringSubmatch(line)
			if lm == nil {
				return listing{}, fmt.Errorf("item %s: want a file:line | kind line, got %q", cur.Name, line)
			}
			fields := strings.Split(lm[2], " | ")
			cur.Loc, cur.Kind, cur.Extra, wantLoc = lm[1], fields[0], fields[1:], false
		case line == "":
			cur = nil
		case cur != nil && strings.HasPrefix(line, "hints: "):
			cur.Hints = strings.TrimPrefix(line, "hints: ")
		case cur != nil && strings.HasPrefix(line, "  "):
			cur.Sig = append(cur.Sig, strings.TrimPrefix(line, "  "))
		case len(l.Items) == 0:
			l.Head = append(l.Head, line)
		default:
			pending = append(pending, line)
		}
	}
	l.Tail = pending
	if wantLoc {
		return listing{}, fmt.Errorf("item %s has no location line", cur.Name)
	}
	if len(l.Items) == 0 {
		return listing{}, fmt.Errorf("no items in %q", s)
	}
	return l, nil
}

// The four shapes the bench compares.
const (
	armText    = "text"
	armOneLine = "oneline"
	armTOON    = "toon"
	armJSON    = "json"
)

var arms = []string{armText, armOneLine, armTOON, armJSON}

// render prints l in one arm's shape. text is the capture itself: render
// rebuilds it so a test can prove the parse dropped nothing.
func render(arm string, l listing) (string, error) {
	switch arm {
	case armText:
		return renderText(l), nil
	case armOneLine:
		return renderOneLine(l), nil
	case armTOON:
		return renderTOON(l), nil
	case armJSON:
		b, err := json.Marshal(l)
		return string(b) + "\n", err
	}
	return "", fmt.Errorf("unknown arm %q (want one of %s)", arm, strings.Join(arms, ", "))
}

func renderText(l listing) string {
	var b strings.Builder
	for _, n := range l.Head {
		b.WriteString(n + "\n")
	}
	for i := range l.Items {
		it := &l.Items[i]
		if i > 0 {
			b.WriteString("\n")
		}
		for _, n := range it.Section {
			b.WriteString(n + "\n\n")
		}
		fmt.Fprintf(&b, "# %s [%s]\n%s\n", it.Name, it.ID, strings.Join(append([]string{it.Loc, it.Kind}, it.Extra...), " | "))
		if it.Hints != "" {
			b.WriteString("hints: " + it.Hints + "\n")
		}
		for _, s := range it.Sig {
			b.WriteString("  " + s + "\n")
		}
	}
	if len(l.Tail) > 0 {
		b.WriteString("\n")
	}
	for _, n := range l.Tail {
		b.WriteString(n + "\n")
	}
	return b.String()
}

// renderOneLine is one line per item: name [id] file:line kind sig, then the
// item's extra fields.
func renderOneLine(l listing) string {
	var b strings.Builder
	for _, n := range l.Head {
		b.WriteString(n + "\n")
	}
	for i := range l.Items {
		it := &l.Items[i]
		for _, n := range it.Section {
			b.WriteString(n + "\n")
		}
		parts := []string{it.Name, "[" + it.ID + "]", it.Loc, it.Kind}
		if len(it.Sig) > 0 {
			parts = append(parts, strings.Join(it.Sig, " "))
		}
		line := strings.Join(parts, " ")
		for _, e := range it.Extra {
			line += " · " + e
		}
		if it.Hints != "" {
			line += " · hints: " + it.Hints
		}
		b.WriteString(line + "\n")
	}
	for _, n := range l.Tail {
		b.WriteString(n + "\n")
	}
	return b.String()
}

// renderTOON prints the notes as a primitive array and the items as one
// tabular array. A column every item leaves empty is dropped.
func renderTOON(l listing) string {
	var b strings.Builder
	if notes := append(slices.Clone(l.Head), l.Tail...); len(notes) > 0 {
		q := make([]string, len(notes))
		for i, n := range notes {
			q[i] = toonValue(n)
		}
		fmt.Fprintf(&b, "notes[%d]: %s\n", len(notes), strings.Join(q, ","))
	}
	cols := []struct {
		name string
		get  func(*item) string
	}{
		{"name", func(it *item) string { return it.Name }},
		{"id", func(it *item) string { return it.ID }},
		{"loc", func(it *item) string { return it.Loc }},
		{"kind", func(it *item) string { return it.Kind }},
		{"detail", func(it *item) string { return strings.Join(it.Sig, " ") }},
		{"extra", func(it *item) string { return strings.Join(it.Extra, " · ") }},
		{"hints", func(it *item) string { return it.Hints }},
		{"section", func(it *item) string { return strings.Join(it.Section, " · ") }},
	}
	var names []string
	var gets []func(*item) string
	for _, c := range cols {
		for i := range l.Items {
			it := &l.Items[i]
			if c.get(it) != "" {
				names, gets = append(names, c.name), append(gets, c.get)
				break
			}
		}
	}
	fmt.Fprintf(&b, "items[%d]{%s}:\n", len(l.Items), strings.Join(names, ","))
	for i := range l.Items {
		it := &l.Items[i]
		row := make([]string, len(gets))
		for i, g := range gets {
			row[i] = toonValue(g(it))
		}
		b.WriteString("  " + strings.Join(row, ",") + "\n")
	}
	return b.String()
}

// toonValue quotes a TOON string when the comma delimiter, a colon, a quote, a
// bracket or edge space would make it ambiguous unquoted.
func toonValue(s string) string {
	if s != "" && s == strings.TrimSpace(s) && !strings.ContainsAny(s, `,:"\[]{}`) {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
