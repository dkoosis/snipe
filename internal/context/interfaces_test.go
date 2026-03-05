package context

import (
	"os"
	"testing"
)

func TestExtractMethodNamesFromSource(t *testing.T) {
	content := `package fake

type Greeter interface {
	Hello() string
	Goodbye(name string) error
	// comment
	unexportedHelper()
}
`
	f := writeTempGoFile(t, content)
	methods := extractMethodNamesFromSource(f, 3, 8)

	if len(methods) != 2 {
		t.Fatalf("expected 2 methods, got %d: %v", len(methods), methods)
	}
	if methods[0] != "Hello" {
		t.Errorf("expected Hello, got %s", methods[0])
	}
	if methods[1] != "Goodbye" {
		t.Errorf("expected Goodbye, got %s", methods[1])
	}
}

func TestExtractMethodNamesFromSource_SkipsEmbedded(t *testing.T) {
	content := `package fake

type ReadWriter interface {
	io.Reader
	Write(p []byte) (n int, err error)
}
`
	f := writeTempGoFile(t, content)
	methods := extractMethodNamesFromSource(f, 3, 6)
	if len(methods) != 1 || methods[0] != "Write" {
		t.Errorf("expected [Write], got %v", methods)
	}
}

func writeTempGoFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.go")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
