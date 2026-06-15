package cmd

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/google/go-cmp/cmp"
)

var update = flag.Bool("update", false, "update golden files")

type helpCase struct {
	name string
	args []string
}

func TestHelpGolden(t *testing.T) {
	cases := collectHelpCases()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			stdout := captureHelp(t, tc.args)

			got := normalizeHelp(stdout)
			goldenPath := filepath.Join("testdata", "help", goldenFileName(tc.args))
			if *update {
				writeGolden(t, goldenPath, got)
				return
			}

			want := readGolden(t, goldenPath)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("help output mismatch for %q (-want +got):\n%s", strings.Join(tc.args, " "), diff)
			}
		})
	}
}

// captureHelp runs `snipe <args> --help` through kong and returns the help
// text. Help output is written to a buffer; Exit is a no-op so the help dump
// (which kong terminates with os.Exit) does not kill the test process.
func captureHelp(t *testing.T, args []string) string {
	t.Helper()

	var buf bytes.Buffer
	cli := &CLI{}
	parser, err := kong.New(cli,
		kong.Name("snipe"),
		kong.Description(rootHelp),
		kong.Writers(&buf, &buf),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		t.Fatalf("build kong parser: %v", err)
	}

	full := append(append([]string(nil), args...), "--help")
	// kong's --help handler writes help then calls Exit. With a no-op Exit the
	// parser keeps going and may report a downstream error (e.g. a missing
	// required positional); the help text is already in buf, so a non-empty
	// buffer means help was produced and any trailing parse error is benign.
	if _, err := parser.Parse(full); err != nil && buf.Len() == 0 {
		t.Fatalf("parse %q --help: %v", strings.Join(args, " "), err)
	}
	return buf.String()
}

// collectHelpCases enumerates the root plus every non-hidden command path from
// the kong grammar.
func collectHelpCases() []helpCase {
	cli := &CLI{}
	parser, err := kong.New(cli, kong.Name("snipe"))
	if err != nil {
		panic(err)
	}

	cases := []helpCase{{name: "root", args: nil}}
	var walk func(nodes []*kong.Node, path []string)
	walk = func(nodes []*kong.Node, path []string) {
		for _, n := range nodes {
			if n.Hidden || n.Name == "" {
				continue
			}
			childPath := append(append([]string(nil), path...), n.Name)
			name := strings.Join(childPath, " ")
			cases = append(cases, helpCase{name: name, args: childPath})
			walk(n.Children, childPath)
		}
	}
	walk(parser.Model.Children, nil)
	return cases
}

func normalizeHelp(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n") + "\n"
}

func goldenFileName(args []string) string {
	if len(args) == 0 {
		return "root.golden"
	}
	return fmt.Sprintf("%s.golden", strings.Join(args, "-"))
}

func readGolden(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	return normalizeHelp(string(data))
}

func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir golden dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}
}
