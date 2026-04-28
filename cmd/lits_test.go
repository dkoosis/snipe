package cmd

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/store"
)

func TestLitsCmd_Registered(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "lits <value>" {
			return
		}
	}
	t.Error("lits command not registered")
}

func TestLitsCommand_RequiresExactlyOneArgument(t *testing.T) {
	stdout, stderr, err := executeCommand(newRootCommandForTest(t), "lits")
	if err == nil {
		t.Fatal("expected error when lits is called without required argument")
	}
	if !strings.Contains(stdout, "Usage:") {
		t.Fatalf("stdout = %q, want usage output", stdout)
	}
	if !strings.Contains(stderr, "accepts 1 arg(s)") {
		t.Fatalf("stderr = %q, want it to mention required arg count", stderr)
	}
}

func TestLitsCommand_ReturnsMissingIndexError_WhenIndexDoesNotExist(t *testing.T) {
	t.Chdir(t.TempDir())

	stdout, stderr, err := executeCommandWithStdIO(t, newRootCommandForTest(t), "lits", "MY_KEY")
	if err == nil {
		t.Fatal("expected error when index is missing")
	}
	if !strings.Contains(stderr, "index missing") {
		t.Fatalf("stderr = %q, want index missing message", stderr)
	}
	if !strings.Contains(stdout, "No index found") {
		t.Fatalf("stdout = %q, want missing index guidance", stdout)
	}
}

func TestLitsCommand_ReturnsNotFoundResponse_WhenLiteralIsAbsent(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	createLitsIndex(t, root, []literalRefRow{{
		id:          "aaaa1111",
		value:       "EXISTING_KEY",
		name:        "EXISTING_KEY",
		kind:        "env",
		filePath:    filepath.Join(root, "main.go"),
		filePathRel: "main.go",
		line:        3,
		col:         10,
		snippet:     `os.Getenv("EXISTING_KEY")`,
	}})

	stdout, stderr, err := executeCommandWithStdIO(t, newRootCommandForTest(t), "lits", "MISSING_KEY")
	if err != nil {
		t.Fatalf("expected nil error for not found response envelope, got %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, `error: no string refs found for "MISSING_KEY"`) {
		t.Fatalf("stdout = %q, want not-found message", stdout)
	}
}

func TestLitsCommand_AppliesValueMatchingAndPagination(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	createLitsIndex(t, root, []literalRefRow{
		{
			id:          "id-001",
			value:       "TARGET",
			name:        "FIRST_TARGET",
			kind:        "env",
			filePath:    filepath.Join(root, "a.go"),
			filePathRel: "a.go",
			line:        2,
			col:         7,
			snippet:     `os.Getenv("TARGET")`,
		},
		{
			id:          "id-002",
			value:       "TARGET",
			name:        "SECOND_TARGET",
			kind:        "const",
			filePath:    filepath.Join(root, "b.go"),
			filePathRel: "b.go",
			line:        9,
			col:         3,
			snippet:     `const second = "TARGET"`,
		},
		{
			id:          "id-003",
			value:       "OTHER",
			name:        "OTHER",
			kind:        "const",
			filePath:    filepath.Join(root, "c.go"),
			filePathRel: "c.go",
			line:        12,
			col:         1,
			snippet:     `const other = "OTHER"`,
		},
	})

	stdout, stderr, err := executeCommandWithStdIO(t, newRootCommandForTest(t), "--offset", "1", "--limit", "1", "lits", "TARGET")
	if err != nil {
		t.Fatalf("lits TARGET returned error: %v", err)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "SECOND_TARGET") {
		t.Fatalf("stdout = %q, want paginated second match", stdout)
	}
	if strings.Contains(stdout, "FIRST_TARGET") {
		t.Fatalf("stdout = %q, did not expect first match after offset", stdout)
	}
	if strings.Contains(stdout, "OTHER") {
		t.Fatalf("stdout = %q, did not expect non-matching value", stdout)
	}
}

type literalRefRow struct {
	id          string
	value       string
	name        string
	kind        string
	filePath    string
	filePathRel string
	line        int
	col         int
	enclosingID string
	snippet     string
}

func createLitsIndex(t *testing.T, root string, rows []literalRefRow) {
	t.Helper()

	indexPath := store.DefaultIndexPath(root)
	s, err := store.Open(indexPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	for _, row := range rows {
		_, err := s.DB().Exec(`
			INSERT INTO string_refs (id, value, name, kind, file_path, file_path_rel, line, col, enclosing_id, snippet)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.id, row.value, row.name, row.kind, row.filePath, row.filePathRel, row.line, row.col, nullableString(row.enclosingID), nullableString(row.snippet))
		if err != nil {
			t.Fatalf("insert string_ref %q: %v", row.id, err)
		}
	}
}

func nullableString(s string) any {
	if s == "" {
		return sql.NullString{}
	}
	return s
}

func executeCommandWithStdIO(t *testing.T, root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	root.SetArgs(args)
	_, err = root.ExecuteC()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr

	stdoutBytes, readErr := io.ReadAll(stdoutR)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	stderrBytes, readErr := io.ReadAll(stderrR)
	if readErr != nil {
		t.Fatalf("read stderr: %v", readErr)
	}
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return string(stdoutBytes), string(stderrBytes), err
}
