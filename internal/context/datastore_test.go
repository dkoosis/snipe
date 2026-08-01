package context

import (
	"strings"
	"testing"
)

func TestDetectDataStores_NotesStore(t *testing.T) {
	got := DetectDataStores("testdata/datastore/notes-store")
	if len(got) != 1 {
		t.Fatalf("want 1 data store, got %d: %+v", len(got), got)
	}
	s := got[0]
	if !strings.HasSuffix(s.Source, "store.go") {
		t.Errorf("source: want ends with store.go, got %q", s.Source)
	}
	if !strings.Contains(s.Dir, `"notes"`) {
		t.Errorf("dir: want mention of notes dir, got %q", s.Dir)
	}
	if !strings.Contains(s.Pattern, "id") || !strings.Contains(s.Pattern, ".md") {
		t.Errorf("pattern: want computed filename mentioning id + .md, got %q", s.Pattern)
	}
}

func TestDetectDataStores_FixedFileOnly(t *testing.T) {
	// A directory holding one fixed-name file (e.g. session.json) is a state
	// file, not a data store — MkdirAll+WriteFile alone isn't the signal;
	// a computed per-record filename is.
	got := DetectDataStores("testdata/datastore/fixed-file-only")
	if len(got) != 0 {
		t.Fatalf("want 0 data stores for a fixed-filename store, got %d: %+v", len(got), got)
	}
}

func TestDetectDataStores_None(t *testing.T) {
	got := DetectDataStores("testdata/datastore/none")
	if len(got) != 0 {
		t.Fatalf("want 0 data stores, got %d: %+v", len(got), got)
	}
}

func TestDetectDataStores_SnipeItself(t *testing.T) {
	// snipe's own repo has one real hit: cmd/diagram.go's writeDiagramDocTo,
	// which MkdirAlls docs/diagrams/ then writes <name>.md per diagram type
	// (sn-l1kh.1) — a directory of documents, the exact pattern this
	// detector targets.
	got := DetectDataStores("../..")
	var found bool
	for _, s := range got {
		if strings.Contains(s.Source, "cmd/diagram.go") {
			found = true
			if !strings.Contains(s.Dir, "diagrams") {
				t.Errorf("dir: want mention of diagrams, got %q", s.Dir)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected to find cmd/diagram.go's docs/diagrams store; got: %+v", got)
	}
}
