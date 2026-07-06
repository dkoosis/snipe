package risk

import (
	"reflect"
	"testing"
)

func TestParseDiff_ExtractsHeadSideRanges_When_HunksPresent(t *testing.T) {
	t.Parallel()

	// A two-file diff: one modified file with two hunks, one added file.
	stream := `diff --git a/internal/store/store.go b/internal/store/store.go
index 1111111..2222222 100644
--- a/internal/store/store.go
+++ b/internal/store/store.go
@@ -10,0 +11,3 @@ func Open() {
+	a()
+	b()
+	c()
@@ -40,2 +43 @@ func Close() {
+	d()
diff --git a/cmd/new.go b/cmd/new.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/cmd/new.go
@@ -0,0 +1,2 @@
+package cmd
+
`

	got := parseDiff(stream)
	want := []FileChange{
		{Path: "internal/store/store.go", LineRanges: [][2]int{{11, 13}, {43, 43}}},
		{Path: "cmd/new.go", LineRanges: [][2]int{{1, 2}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDiff mismatch:\n got=%+v\nwant=%+v", got, want)
	}
}

func TestParseDiff_SkipsPureDeletions_When_HeadCountZero(t *testing.T) {
	t.Parallel()

	// A file deleted entirely (+++ /dev/null) and a hunk that only removes lines
	// (+0 count) contribute no head-side ranges.
	stream := `diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package gone
diff --git a/trim.go b/trim.go
--- a/trim.go
+++ b/trim.go
@@ -5,3 +4,0 @@ func f() {
-	old()
`

	got := parseDiff(stream)
	// gone.go maps to /dev/null → dropped; trim.go has a hunk with +0 count → no ranges.
	if len(got) != 0 {
		t.Fatalf("expected no head-side changes, got %+v", got)
	}
}

func TestChangedGoFiles_FiltersToGo(t *testing.T) {
	t.Parallel()

	changes := []FileChange{
		{Path: "cmd/risk.go"},
		{Path: "README.md"},
		{Path: "internal/risk/diff.go"},
		{Path: "go.mod"},
	}
	got := changedGoFiles(changes)
	want := []string{"cmd/risk.go", "internal/risk/diff.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedGoFiles=%v want %v", got, want)
	}
}
