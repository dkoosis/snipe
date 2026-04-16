package context

import (
	"strings"
	"testing"
)

func TestDetectDBSchemas_TopLevelSQL(t *testing.T) {
	got := DetectDBSchemas("testdata/dbschema/top-level-sql")
	if len(got) != 1 {
		t.Fatalf("want 1 schema, got %d", len(got))
	}
	s := got[0]
	if !strings.HasSuffix(s.Source, "schema.sql") {
		t.Errorf("source: want ends with schema.sql, got %q", s.Source)
	}
	if !strings.Contains(s.DDL, "CREATE TABLE queue") {
		t.Errorf("DDL missing queue table: %q", s.DDL)
	}
	if !strings.Contains(s.DDL, "CREATE INDEX idx_queue_path") {
		t.Errorf("DDL missing queue index: %q", s.DDL)
	}
}

func TestDetectDBSchemas_EmbeddedGo(t *testing.T) {
	got := DetectDBSchemas("testdata/dbschema/embedded-go")
	if len(got) != 1 {
		t.Fatalf("want 1 schema, got %d", len(got))
	}
	s := got[0]
	if !strings.HasSuffix(s.Source, "store.go") {
		t.Errorf("source: want ends with store.go, got %q", s.Source)
	}
	if !strings.Contains(s.DDL, "CREATE TABLE IF NOT EXISTS symbols") {
		t.Errorf("DDL missing symbols table: %q", s.DDL)
	}
	if !strings.Contains(s.DDL, "CREATE INDEX idx_symbols_name") {
		t.Errorf("DDL missing index: %q", s.DDL)
	}
}

func TestDetectDBSchemas_MultiSchemaGo(t *testing.T) {
	got := DetectDBSchemas("testdata/dbschema/multi-schema-go")
	if len(got) != 3 {
		t.Fatalf("want 3 schemas (store, cclog, metrics), got %d", len(got))
	}

	names := make(map[string]bool)
	for _, s := range got {
		names[s.Name] = true
		if s.DDL == "" {
			t.Errorf("schema %q has empty DDL", s.Name)
		}
	}
	for _, want := range []string{"store", "cclog", "metrics"} {
		if !names[want] {
			t.Errorf("missing schema %q; got names: %v", want, names)
		}
	}
}

func TestDetectDBSchemas_Migrations(t *testing.T) {
	got := DetectDBSchemas("testdata/dbschema/migrations")
	if len(got) != 1 {
		t.Fatalf("want 1 coalesced migrations schema, got %d", len(got))
	}
	s := got[0]
	if !strings.Contains(s.Source, "migrations") {
		t.Errorf("source: want contains 'migrations', got %q", s.Source)
	}
	if !strings.Contains(s.DDL, "CREATE TABLE users") {
		t.Errorf("DDL missing users (migration 1): %q", s.DDL)
	}
	if !strings.Contains(s.DDL, "CREATE TABLE posts") {
		t.Errorf("DDL missing posts (migration 2): %q", s.DDL)
	}
	usersIdx := strings.Index(s.DDL, "CREATE TABLE users")
	postsIdx := strings.Index(s.DDL, "CREATE TABLE posts")
	if usersIdx > postsIdx {
		t.Errorf("migrations not in order: users at %d, posts at %d", usersIdx, postsIdx)
	}
}

func TestDetectDBSchemas_None(t *testing.T) {
	got := DetectDBSchemas("testdata/dbschema/none")
	if len(got) != 0 {
		t.Fatalf("want 0 schemas, got %d: %+v", len(got), got)
	}
}

func TestDetectDBSchemas_SnipeItself(t *testing.T) {
	got := DetectDBSchemas("../..")
	if len(got) == 0 {
		t.Fatalf("want at least 1 schema from snipe's own repo, got 0")
	}
	var found bool
	for _, s := range got {
		if strings.Contains(s.Source, "store/schema.go") && strings.Contains(s.DDL, "CREATE TABLE IF NOT EXISTS symbols") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find internal/store/schema.go with symbols table; got sources: %v", sources(got))
	}
}

func sources(schemas []DBSchema) []string {
	out := make([]string, len(schemas))
	for i, s := range schemas {
		out[i] = s.Source
	}
	return out
}
