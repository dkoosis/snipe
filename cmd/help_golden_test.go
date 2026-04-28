package cmd

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
)

var update = flag.Bool("update", false, "update golden files")

type helpCase struct {
	name string
	args []string
}

func TestHelpGolden(t *testing.T) {
	cases := collectHelpCases(newRootCommandForTest(t), nil)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := executeCommand(newRootCommandForTest(t), append(tc.args, "--help")...)
			if err != nil {
				t.Fatalf("execute %q --help: %v", strings.Join(tc.args, " "), err)
			}
			if strings.TrimSpace(stderr) != "" {
				t.Fatalf("unexpected stderr for %q --help: %q", strings.Join(tc.args, " "), stderr)
			}

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

func newRootCommandForTest(t *testing.T) *cobra.Command {
	t.Helper()
	return cloneCommandTree(rootCmd)
}

func cloneCommandTree(cmd *cobra.Command) *cobra.Command {
	cloned := *cmd
	cloned.ResetCommands()
	for _, child := range cmd.Commands() {
		cloned.AddCommand(cloneCommandTree(child))
	}
	return &cloned
}

func executeCommand(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer

	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	_, err = root.ExecuteC()

	return outBuf.String(), errBuf.String(), err
}

func collectHelpCases(cmd *cobra.Command, path []string) []helpCase {
	name := strings.Join(path, " ")
	if len(path) == 0 {
		name = "root"
	}

	cases := []helpCase{{name: name, args: append([]string(nil), path...)}}
	for _, child := range cmd.Commands() {
		if child.Hidden {
			continue
		}
		childPath := append(append([]string(nil), path...), child.Name())
		cases = append(cases, collectHelpCases(child, childPath)...)
	}
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
