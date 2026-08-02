//go:build blackbox

package blackbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClassMapFixture creates a repo with one interface (Namer), a base
// struct (Base) embedded by another struct (Widget), and Widget's own field
// plus a method that satisfies Namer — exercising fields, methods, embeds,
// and implements in one compact fixture (sn-l1kh.9 acceptance).
func writeClassMapFixture(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()

	writeFile(t, filepath.Join(repoDir, "go.mod"),
		"module example.com/classmapfix\n\ngo 1.20\n")

	content := `package shapes

// Namer is implemented by anything with a display name.
type Namer interface {
	Name() string
}

// Base is embedded by Widget below.
type Base struct {
	ID string
}

// Widget embeds Base, adds its own field, and implements Namer.
type Widget struct {
	Base
	Label string
}

// Name satisfies Namer.
func (w *Widget) Name() string {
	return w.Label
}
`
	writeFile(t, filepath.Join(repoDir, "shapes", "shapes.go"), content)
	return repoDir
}

// TestGolden_Diagram_ClassMap_DefaultWritesDoc pins the emitDoc default path
// (sn-l1kh.9): no --format writes docs/diagrams/class-map.md, mirroring
// arch/flow/lifecycle's default (sn-l1kh.1, sn-l1kh.8).
func TestGolden_Diagram_ClassMap_DefaultWritesDoc(t *testing.T) {
	repoDir := writeClassMapFixture(t)
	initGitRepo(t, repoDir)
	indexRepo(t, repoDir)

	stdout, stderr, exitCode := runRaw(t, repoDir, "diagram", "class-map")
	if exitCode != 0 {
		t.Fatalf("exit %d stderr=%s", exitCode, string(stderr))
	}
	if strings.Contains(string(stdout), "```d2") {
		t.Errorf("default format must not dump D2 to stdout; got %q", string(stdout))
	}

	docPath := filepath.Join(repoDir, "docs", "diagrams", "class-map.md")
	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	want := []string{
		"struct Widget", "field   Label", "embeds  Base",
		"method  Name() string", "implements  Namer",
		"interface Namer",
	}
	for _, w := range want {
		if !strings.Contains(string(doc), w) {
			t.Errorf("doc missing %q:\n%s", w, doc)
		}
	}
}
