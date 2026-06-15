package cmd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

func TestLitsCmd_Registered(t *testing.T) {
	parser := newTestParser(t)
	for _, n := range parser.Model.Children {
		if n.Name == "lits" {
			return
		}
	}
	t.Error("lits command not registered")
}

func TestLitsCommand_RequiresExactlyOneArgument(t *testing.T) {
	_, _, err := runCLI(t, "lits")
	if err == nil {
		t.Fatal("expected error when lits is called without required argument")
	}
	// kong reports a missing required positional as: expected "<value>"
	if !strings.Contains(err.Error(), "value") {
		t.Fatalf("err = %q, want it to mention the missing value argument", err.Error())
	}
}

func TestLitsCommand_ReturnsMissingIndexError_WhenIndexDoesNotExist(t *testing.T) {
	t.Chdir(t.TempDir())

	stdout, stderr, err := runCLI(t, "lits", "MY_KEY")
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

	stdout, stderr, err := runCLI(t, "lits", "MISSING_KEY")
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

	stdout, stderr, err := runCLI(t, "--offset", "1", "--limit", "1", "lits", "TARGET")
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
