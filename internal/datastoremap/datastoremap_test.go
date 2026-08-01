package datastoremap

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dkoosis/snipe/internal/c4"
	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/lifecycle"
)

func TestFilterTestPackages(t *testing.T) {
	in := []c4.Datastore{
		{Name: "SQLite", Package: "example.com/repo/internal/store"},
		{Name: "SQLite", Package: "example.com/repo/internal/query_test"},
		{Name: "SQL", Package: "example.com/repo/internal/store"},
	}
	got := FilterTestPackages(in)
	assert.Len(t, got, 2)
	for _, d := range got {
		assert.NotContains(t, d.Package, "_test")
	}
}

func TestGroupByPackage_PrefersSpecificDriverOverGenericSQL(t *testing.T) {
	in := []c4.Datastore{
		{Name: "SQL", Package: "example.com/repo/internal/store", Driver: "database/sql"},
		{Name: "SQLite", Package: "example.com/repo/internal/store", Driver: "modernc.org/sqlite"},
	}
	got := GroupByPackage(in)
	if assert.Len(t, got, 1) {
		assert.Equal(t, "SQLite", got[0].Name)
	}
}

func TestGroupByPackage_DistinctPackagesKeepBothRows(t *testing.T) {
	in := []c4.Datastore{
		{Name: "SQLite", Package: "example.com/repo/internal/store"},
		{Name: "Redis", Package: "example.com/repo/internal/cache"},
	}
	got := GroupByPackage(in)
	assert.Len(t, got, 2)
}

func TestMatchSchema(t *testing.T) {
	modulePath := "example.com/repo"
	datastores := []c4.Datastore{
		{Name: "SQLite", Package: "example.com/repo/internal/store"},
	}

	t.Run("directory match resolves the owning package", func(t *testing.T) {
		schema := context.DBSchema{Source: "internal/store/schema.go", Name: "store"}
		pkg, ok := MatchSchema(schema, datastores, modulePath)
		assert.True(t, ok)
		assert.Equal(t, "example.com/repo/internal/store", pkg)
	})

	t.Run("repo-root schema has no owning package", func(t *testing.T) {
		schema := context.DBSchema{Source: "schema.sql", Name: "schema"}
		_, ok := MatchSchema(schema, datastores, modulePath)
		assert.False(t, ok)
	})

	t.Run("directory that matches no datastore package", func(t *testing.T) {
		schema := context.DBSchema{Source: "migrations/0001_init.sql", Name: "migrations"}
		_, ok := MatchSchema(schema, datastores, modulePath)
		assert.False(t, ok)
	})
}

func TestBucketRoles(t *testing.T) {
	classifications := []lifecycle.Classification{
		{EnclosingName: "LoadNug", FileRel: "store/read.go", Line: 10, Role: lifecycle.RoleRead},
		{EnclosingName: "SaveNug", FileRel: "store/write.go", Line: 20, Role: lifecycle.RoleCreate},
		{EnclosingName: "UpdateNug", FileRel: "store/write.go", Line: 30, Role: lifecycle.RoleMutate},
		{EnclosingName: "DeleteNug", FileRel: "store/write.go", Line: 5, Role: lifecycle.RoleDelete},
		{EnclosingName: "TestLoadNug", FileRel: "store/read_test.go", Line: 1, Role: lifecycle.RoleRead, IsTestFile: true},
		{EnclosingName: "unrelated", FileRel: "store/misc.go", Line: 1, Role: lifecycle.RoleTypeUse},
	}

	reads, writes, unclear := BucketRoles(classifications)

	assert.Len(t, reads, 1)
	assert.Equal(t, "LoadNug", reads[0].Func)

	if assert.Len(t, writes, 3) {
		// sorted by file then line: write.go:5 (Delete), write.go:20 (Save), write.go:30 (Update)
		assert.Equal(t, "DeleteNug", writes[0].Func)
		assert.Equal(t, "SaveNug", writes[1].Func)
		assert.Equal(t, "UpdateNug", writes[2].Func)
	}

	assert.Equal(t, 1, unclear)
}

func TestBucketRoles_Empty(t *testing.T) {
	reads, writes, unclear := BucketRoles(nil)
	assert.Empty(t, reads)
	assert.Empty(t, writes)
	assert.Equal(t, 0, unclear)
}
