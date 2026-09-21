//go:build blackbox

package blackbox

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

// fakeVoyage stands in for the Voyage embeddings endpoint and records every
// text sent for embedding, so a test can assert what an index run re-embedded
// (sn-1ewy).
type fakeVoyage struct {
	mu    sync.Mutex
	texts []string
	srv   *httptest.Server
}

func newFakeVoyage(t *testing.T) *fakeVoyage {
	t.Helper()
	f := &fakeVoyage{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.texts = append(f.texts, req.Input...)
		f.mu.Unlock()
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"object": "embedding", "embedding": []float32{0.1, 0.2, 0.3, 0.4}, "index": i}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// drain returns the texts recorded since the last drain and clears them.
func (f *fakeVoyage) drain() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.texts
	f.texts = nil
	return out
}

func (f *fakeVoyage) env() []string {
	return append(envWithout("VOYAGE_API_KEY", "VOYAGE_MODEL", "VOYAGE_API_URL"),
		"VOYAGE_API_KEY=test-key", "VOYAGE_API_URL="+f.srv.URL)
}

// textFor returns the recorded texts that begin with the given symbol name.
func textFor(texts []string, name string) []string {
	var out []string
	for _, tx := range texts {
		if strings.HasPrefix(tx, name+" ") {
			out = append(out, tx)
		}
	}
	return out
}

func embeddedNames(t *testing.T, repoDir string) map[string]bool {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(repoDir, ".snipe", "index.db"))
	if err != nil {
		t.Fatalf("open index db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT s.name FROM embeddings e JOIN symbols s ON s.id = e.symbol_id`)
	if err != nil {
		t.Fatalf("query embeddings: %v", err)
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names[n] = true
	}
	return names
}

func writeEmbedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/emb\n\ngo 1.20\n")
	writeFile(t, filepath.Join(dir, "helper.go"), `package emb

// Helper does the shared work.
func Helper() int { return 1 }

// Sibling lives beside Helper.
func Sibling() int { return 2 }
`)
	writeFile(t, filepath.Join(dir, "other.go"), `package emb

// Unrelated is untouched by every edit in these tests.
func Unrelated() int { return 3 }
`)
	initGitRepo(t, dir)
	return dir
}

func mustIndex(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runWithEnv(t, dir, env, append([]string{"index"}, args...)...)
	if code != 0 {
		t.Fatalf("index %v exit %d stderr=%s stdout=%s", args, code, stderr, stdout)
	}
	return string(stderr)
}

// An incremental update embeds a new symbol, refreshes the callee whose caller
// set changed in an untouched file, re-embeds the untouched-by-text siblings of
// an edited file (the incremental write drops their vectors), and leaves
// unrelated symbols alone.
func TestIncrementalIndex_Reembeds(t *testing.T) {
	v := newFakeVoyage(t)
	dir := writeEmbedFixture(t)
	env := v.env()

	mustIndex(t, dir, env, "--embed-mode=realtime")
	before := embeddedNames(t, dir)
	for _, n := range []string{"Helper", "Sibling", "Unrelated"} {
		if !before[n] {
			t.Fatalf("full index did not embed %s: %v", n, before)
		}
	}
	v.drain()

	// New file: adds a symbol and a caller of Helper (helper.go untouched).
	writeFile(t, filepath.Join(dir, "caller.go"), `package emb

// Caller uses Helper.
func Caller() int { return Helper() }
`)
	mustIndex(t, dir, env)
	got := v.drain()
	if len(textFor(got, "Caller")) == 0 {
		t.Errorf("new symbol Caller was not embedded; sent: %q", got)
	}
	helper := textFor(got, "Helper")
	if len(helper) == 0 || !strings.Contains(helper[0], "called by: Caller") {
		t.Errorf("Helper's vector was not refreshed with its new caller; sent: %q", got)
	}
	for _, n := range []string{"Sibling", "Unrelated"} {
		if len(textFor(got, n)) != 0 {
			t.Errorf("%s did not change but was re-embedded; sent: %q", n, got)
		}
	}
	after := embeddedNames(t, dir)
	if !after["Caller"] || !after["Helper"] || !after["Sibling"] || !after["Unrelated"] {
		t.Errorf("embeddings after adding Caller = %v", after)
	}

	// Edit a file: its symbols lose their stored vectors in the incremental
	// write, so all of them must come back.
	writeFile(t, filepath.Join(dir, "helper.go"), `package emb

// Helper does the shared work.
func Helper() int { return 1 }

// Sibling lives beside Helper, now edited.
func Sibling() int { return 2 }
`)
	mustIndex(t, dir, env)
	got = v.drain()
	for _, n := range []string{"Helper", "Sibling"} {
		if len(textFor(got, n)) == 0 {
			t.Errorf("%s in the edited file was not re-embedded; sent: %q", n, got)
		}
	}
	if len(textFor(got, "Unrelated")) != 0 {
		t.Errorf("Unrelated did not change but was re-embedded; sent: %q", got)
	}
	final := embeddedNames(t, dir)
	for _, n := range []string{"Helper", "Sibling", "Caller", "Unrelated"} {
		if !final[n] {
			t.Errorf("%s has no embedding after editing its file: %v", n, final)
		}
	}
}

// --embed-mode=off makes no embedding calls on the incremental path.
func TestIncrementalIndex_EmbedOffMakesNoCalls(t *testing.T) {
	v := newFakeVoyage(t)
	dir := writeEmbedFixture(t)
	env := v.env()

	mustIndex(t, dir, env, "--embed-mode=realtime")
	v.drain()

	writeFile(t, filepath.Join(dir, "caller.go"), `package emb

// Caller uses Helper.
func Caller() int { return Helper() }
`)
	mustIndex(t, dir, env, "--embed-mode=off")
	if got := v.drain(); len(got) != 0 {
		t.Errorf("--embed-mode=off sent %d texts on the incremental path: %q", len(got), got)
	}
}

// Without credentials the incremental path neither calls out nor fails.
func TestIncrementalIndex_NoCredentialsMakesNoCalls(t *testing.T) {
	v := newFakeVoyage(t)
	dir := writeEmbedFixture(t)

	mustIndex(t, dir, v.env(), "--embed-mode=realtime")
	v.drain()

	writeFile(t, filepath.Join(dir, "caller.go"), `package emb

// Caller uses Helper.
func Caller() int { return Helper() }
`)
	bare := append(envWithout("VOYAGE_API_KEY", "VOYAGE_MODEL", "VOYAGE_API_URL"), "HOME="+t.TempDir())
	mustIndex(t, dir, bare)
	if got := v.drain(); len(got) != 0 {
		t.Errorf("no-credentials incremental run sent %d texts: %q", len(got), got)
	}
}
