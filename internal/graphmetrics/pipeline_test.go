package graphmetrics

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

func TestComponentCount(t *testing.T) {
	set := func(ids ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			m[id] = struct{}{}
		}
		return m
	}
	tests := []struct {
		name  string
		nodes map[string]struct{}
		edges [][2]string
		want  int
	}{
		{"empty", set(), nil, 0},
		{"singleton", set("a"), nil, 1},
		{"disconnected", set("a", "b", "c"), nil, 3},
		{"chain", set("a", "b", "c"), [][2]string{{"a", "b"}, {"b", "c"}}, 1},
		{"direction ignored", set("a", "b", "c"), [][2]string{{"b", "a"}, {"b", "c"}}, 1},
		{"two clusters", set("a", "b", "c", "d"), [][2]string{{"a", "b"}, {"c", "d"}}, 2},
		{"edge to unknown node ignored", set("a", "b"), [][2]string{{"a", "x"}, {"x", "b"}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := componentCount(tt.nodes, tt.edges); got != tt.want {
				t.Errorf("componentCount = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCompute_PersistsMetricsPastFailedStep feeds Compute a store with no
// go.mod at repo_root, so the import-graph steps fail, and checks that the
// later steps (cohesion, file complexity) still persist their rows.
func TestCompute_PersistsMetricsPastFailedStep(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(filepath.Join(root, "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.SetMeta("repo_root", root); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	db := s.DB()
	insertSym := func(id, name, kind, pkg, file string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO symbols (id,name,kind,pkg_path,file_path,line_start,col_start,line_end,col_end) VALUES (?,?,?,?,?,1,1,1,1)`,
			id, name, kind, pkg, file); err != nil {
			t.Fatalf("insert sym %s: %v", id, err)
		}
	}
	insertRef := func(id, target, enclosing string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO refs (id,symbol_id,file_path,line,col,enclosing_id) VALUES (?,?,?,1,1,?)`,
			id, target, "a.go", enclosing); err != nil {
			t.Fatalf("insert ref %s: %v", id, err)
		}
	}

	// Package p: f1→f2 connected, f3 alone → LCOM4 = 2.
	// One exported interface and one exported struct → abstractness = 0.5.
	insertSym("f1", "f1", "func", "m/p", "a.go")
	insertSym("f2", "f2", "func", "m/p", "a.go")
	insertSym("f3", "f3", "func", "m/p", "b.go")
	insertSym("i1", "Reader", "interface", "m/p", "a.go")
	insertSym("s1", "File", "struct", "m/p", "a.go")
	insertSym("t1", "helper", "func", "m/p", "a_test.go")
	insertRef("r1", "f2", "f1")
	insertRef("r2", "f3", "t1") // test caller must not connect f3

	symbols := []index.Symbol{
		{Kind: index.KindFunc, FilePath: filepath.Join(root, "a.go"), Cyclo: 3, Cognitive: 4},
		{Kind: index.KindFunc, FilePath: filepath.Join(root, "a.go"), Cyclo: 5, Cognitive: 1},
		{Kind: index.KindFunc, FilePath: filepath.Join(root, "a_test.go"), Cyclo: 9, Cognitive: 9},
	}

	var log bytes.Buffer
	Compute(s, symbols, &log)

	if !strings.Contains(log.String(), "Warning: SCC computation failed") {
		t.Errorf("want a warning for the import-graph step with no go.mod, got log:\n%s", log.String())
	}

	want := []struct {
		graph, metric, node string
		value               float64
	}{
		{"imports", "lcom4", "m/p", 2},
		{"imports", "abstractness", "m/p", 0.5},
		{"files", "cyclo_sum", "a.go", 8},
		{"files", "cyclo_max", "a.go", 5},
		{"files", "cognitive_sum", "a.go", 5},
	}
	for _, w := range want {
		var got float64
		err := db.QueryRow(`SELECT value FROM graph_metrics WHERE graph_kind=? AND metric=? AND node_id=?`,
			w.graph, w.metric, w.node).Scan(&got)
		if err != nil {
			t.Errorf("%s/%s/%s: %v\nlog:\n%s", w.graph, w.metric, w.node, err, log.String())
			continue
		}
		if got != w.value {
			t.Errorf("%s/%s/%s = %v, want %v", w.graph, w.metric, w.node, got, w.value)
		}
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM graph_metrics WHERE graph_kind='files' AND node_id LIKE '%_test.go'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("test files must not get file-complexity rows, got %d", n)
	}
	if NeedBackfill(s) {
		t.Error("Compute should stamp the current file-metrics version")
	}
}
