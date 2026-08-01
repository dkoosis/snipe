package cmd

import (
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/datastoremap"
	"github.com/dkoosis/snipe/internal/lifecycle"
)

func TestPickStoreTypes_PrefersHandleName(t *testing.T) {
	types := []pkgType{
		{ID: "1", Name: "FileChurn"},
		{ID: "2", Name: "Store"},
		{ID: "3", Name: "MetricRow"},
	}
	got := pickStoreTypes(types)
	if len(got) != 1 || got[0].Name != "Store" {
		t.Fatalf("want only Store, got %+v", got)
	}
}

func TestPickStoreTypes_NoHandleNameReturnsEmpty(t *testing.T) {
	// c4 flags any package importing bare "database/sql" as a datastore, even
	// one that only takes a *sql.DB parameter rather than owning a store.
	// Without a named handle type, pickStoreTypes must return nothing rather
	// than fall back to every exported type (that would run lifecycle
	// classification against unrelated types and produce noise).
	types := []pkgType{
		{ID: "1", Name: "FileChurn"},
		{ID: "2", Name: "MetricRow"},
	}
	got := pickStoreTypes(types)
	if len(got) != 0 {
		t.Fatalf("want empty result when no handle name matches, got %+v", got)
	}
}

func TestPickStoreTypes_CaseInsensitiveMatch(t *testing.T) {
	types := []pkgType{
		{ID: "1", Name: "DB"},
		{ID: "2", Name: "Other"},
	}
	got := pickStoreTypes(types)
	if len(got) != 1 || got[0].Name != "DB" {
		t.Fatalf("want only DB, got %+v", got)
	}
}

func TestRenderDatastoreSummary_UnmatchedSchemaNotesMissingMap(t *testing.T) {
	stores := []datastoremap.Store{
		{Name: "schema", SchemaSource: "schema.sql", DDL: "CREATE TABLE t (id INTEGER)"},
	}
	got := renderDatastoreSummary(stores)
	if !strings.Contains(got, "read/write map unavailable") {
		t.Fatalf("want unresolved-package note, got:\n%s", got)
	}
	if !strings.Contains(got, "CREATE TABLE t") {
		t.Fatalf("want DDL embedded, got:\n%s", got)
	}
}

func TestRenderDatastoreSummary_MatchedStoreListsReadsAndWrites(t *testing.T) {
	stores := []datastoremap.Store{
		{
			Name:         "SQLite",
			Package:      "example.com/repo/internal/store",
			SchemaSource: "internal/store/schema.go",
			DDL:          "CREATE TABLE nugs (id TEXT)",
			Reads:        []datastoremap.Entry{{Func: "LoadNug", File: "internal/store/read.go", Line: 10, Role: lifecycle.RoleRead}},
			Writes:       []datastoremap.Entry{{Func: "SaveNug", File: "internal/store/write.go", Line: 20, Role: lifecycle.RoleCreate}},
			Unclear:      2,
		},
	}
	got := renderDatastoreSummary(stores)
	for _, want := range []string{"reads (1)", "LoadNug", "writes (1)", "SaveNug", "unclear: 2"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q, got:\n%s", want, got)
		}
	}
}

func TestRenderDatastoreSummary_ListCapTruncates(t *testing.T) {
	var reads []datastoremap.Entry
	for i := 0; i < datastoreListCap+5; i++ {
		reads = append(reads, datastoremap.Entry{Func: "Fn", File: "f.go", Line: i + 1, Role: lifecycle.RoleRead})
	}
	stores := []datastoremap.Store{
		{Name: "SQLite", Package: "example.com/repo/internal/store", Reads: reads},
	}
	got := renderDatastoreSummary(stores)
	if !strings.Contains(got, "+5 more") {
		t.Fatalf("want truncation marker, got:\n%s", got)
	}
}

func TestRenderDatastoreDiagram_NoPackageStillDrawsStoreNode(t *testing.T) {
	stores := []datastoremap.Store{
		{Name: "schema", SchemaSource: "schema.sql"},
	}
	d2 := renderDatastoreDiagram(stores).Render()
	if !strings.Contains(d2, "schema") {
		t.Fatalf("want store node rendered even without a matched package, got:\n%s", d2)
	}
}

func TestRenderDatastoreDiagram_DrawsReadWriteEdges(t *testing.T) {
	stores := []datastoremap.Store{
		{
			Name:    "SQLite",
			Package: "example.com/repo/internal/store",
			Reads:   []datastoremap.Entry{{Func: "LoadNug", File: "read.go", Line: 10, Role: lifecycle.RoleRead}},
			Writes:  []datastoremap.Entry{{Func: "SaveNug", File: "write.go", Line: 20, Role: lifecycle.RoleCreate}},
		},
	}
	d2 := renderDatastoreDiagram(stores).Render()
	if !strings.Contains(d2, "LoadNug") || !strings.Contains(d2, "SaveNug") {
		t.Fatalf("want both read and write function nodes rendered, got:\n%s", d2)
	}
}
