package index_test

import (
	"os"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
)

func TestExtractPackageDocs_NonEmpty(t *testing.T) {
	// Use the project root so we pick up packages that have doc comments.
	// Fall back to "." if we can't find the root.
	dir := "."
	if wd, err := os.Getwd(); err == nil {
		// We're in internal/index, go up two levels to project root
		root := wd + "/../.."
		if _, err := os.Stat(root + "/go.mod"); err == nil {
			dir = root
		}
	}

	result, err := index.Load(index.LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	docs := index.ExtractPackageDocs(result)
	if len(docs) == 0 {
		t.Skip("no package docs found (no packages with doc comments in tree)")
	}
	for _, d := range docs {
		if d.PkgPath == "" {
			t.Error("empty PkgPath in PackageDoc")
		}
		if d.Doc == "" {
			t.Error("empty Doc in PackageDoc")
		}
	}
}
