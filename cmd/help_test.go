package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// TestHelp_EveryCommandAndFlagHasHelp walks the kong grammar and fails for any
// command or flag declared without help text. It replaces per-command help
// snapshots: wording is free to change, an undocumented command is not.
func TestHelp_EveryCommandAndFlagHasHelp(t *testing.T) {
	parser, err := kong.New(&CLI{}, kong.Name("snipe"))
	if err != nil {
		t.Fatalf("build kong parser: %v", err)
	}
	for _, m := range missingHelp(parser.Model.Node) {
		t.Errorf("no help text: %s", m)
	}
}

func TestMissingHelp_ReportsUndocumentedCommandAndFlag(t *testing.T) {
	var cli struct {
		Documented struct{} `cmd:"" help:"Has help"`
		Bare       struct {
			Loud bool
		} `cmd:""`
	}
	parser, err := kong.New(&cli, kong.Name("x"))
	if err != nil {
		t.Fatalf("build kong parser: %v", err)
	}
	got := strings.Join(missingHelp(parser.Model.Node), "; ")
	want := "command bare; flag bare --loud"
	if got != want {
		t.Errorf("missingHelp = %q, want %q", got, want)
	}
}

// TestHelp_RootListsEveryVisibleCommand renders `snipe --help` and checks each
// non-hidden top-level command appears in it.
func TestHelp_RootListsEveryVisibleCommand(t *testing.T) {
	var buf bytes.Buffer
	parser, err := kong.New(&CLI{},
		kong.Name("snipe"),
		kong.Description(rootHelp),
		rootHelpOptions,
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatalf("build kong parser: %v", err)
	}
	// kong's --help handler writes help then calls Exit; with a no-op Exit the
	// parse may go on to report a missing command, which is benign here.
	if _, err := parser.Parse([]string{"--help"}); err != nil && buf.Len() == 0 {
		t.Fatalf("parse --help: %v", err)
	}
	help := buf.String()
	for _, n := range parser.Model.Children {
		if n.Hidden {
			continue
		}
		if !strings.Contains(help, "\n  "+n.Name+" ") {
			t.Errorf("root help does not list command %q", n.Name)
		}
	}
}

// missingHelp returns one entry per command or flag under root that has no
// help text, in grammar order.
func missingHelp(root *kong.Node) []string {
	var out []string
	var walk func(n *kong.Node, path string)
	walk = func(n *kong.Node, path string) {
		for _, f := range n.Flags {
			if f.Help == "" && f.Name != "help" {
				out = append(out, strings.TrimSpace("flag "+path+" --"+f.Name))
			}
		}
		for _, c := range n.Children {
			childPath := strings.TrimSpace(path + " " + c.Name)
			if c.Help == "" {
				out = append(out, "command "+childPath)
			}
			walk(c, childPath)
		}
	}
	walk(root, "")
	return out
}
