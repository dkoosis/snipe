//go:build blackbox

package blackbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T) (repoDir string, paths map[string]string) {
	t.Helper()

	repoDir = t.TempDir()
	paths = make(map[string]string)

	goMod := "module example.com/fixture\n\ngo 1.20\n"
	writeFile(t, filepath.Join(repoDir, "go.mod"), goMod)

	mainContent := `package fixture

import (
	"example.com/fixture/alpha"
	"example.com/fixture/beta"
	"example.com/fixture/greet"
)

type Widget struct {
	Name string
}

func (w *Widget) Do() string {
	return greet.Friendly{}.Hello()
}

func Caller() string {
	// POSITION_CALLEE
	value := Callee()
	return value
}

func AnotherCaller() string {
	// Keep this body big to exercise truncation logic.
	_ = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_ = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	return Callee()
}

func Callee() string {
	return "ok"
}

func UseGreeter(g greet.Greeter) string {
	return g.Hello()
}

func UseAmbiguous() {
	alpha.Ambiguous()
	beta.Ambiguous()
}
`

	mainPath := filepath.Join(repoDir, "main.go")
	writeFile(t, mainPath, mainContent)
	paths["main"] = mainPath

	line, col := positionAfterMarker(t, mainContent, "POSITION_CALLEE", "Callee")
	paths["callee_at"] = fmt.Sprintf("%s:%d:%d", mainPath, line, col)

	greetContent := `package greet

type Greeter interface {
	Hello() string
}

type Friendly struct{}

func (Friendly) Hello() string {
	return "hi"
}

type Rude struct{}

func (Rude) Hello() string {
	return "bye"
}
`

	greetPath := filepath.Join(repoDir, "greet", "greet.go")
	writeFile(t, greetPath, greetContent)
	paths["greet"] = greetPath

	alphaContent := `package alpha

func Ambiguous() string {
	return "alpha"
}
`
	alphaPath := filepath.Join(repoDir, "alpha", "alpha.go")
	writeFile(t, alphaPath, alphaContent)
	paths["alpha"] = alphaPath

	betaContent := `package beta

func Ambiguous() string {
	return "beta"
}
`
	betaPath := filepath.Join(repoDir, "beta", "beta.go")
	writeFile(t, betaPath, betaContent)
	paths["beta"] = betaPath

	return repoDir, paths
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func positionAfterMarker(t *testing.T, content, marker, symbol string) (line, col int) {
	t.Helper()

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines)-1; i++ {
		if strings.Contains(lines[i], marker) {
			idx := strings.Index(lines[i+1], symbol)
			if idx == -1 {
				break
			}
			return i + 2, idx + 1
		}
	}
	for i, text := range lines {
		idx := strings.Index(text, symbol)
		if idx != -1 {
			return i + 1, idx + 1
		}
	}
	t.Fatalf("marker %q with symbol %q not found", marker, symbol)
	return 0, 0
}
