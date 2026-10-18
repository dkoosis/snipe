//go:build blackbox

package blackbox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The binary under test never sees a developer's VOYAGE_* vars, so a blackbox
// run can't upload real Voyage batches (sn-pln6).
func TestSnipeEnv_StripsVoyageVars(t *testing.T) {
	t.Setenv("VOYAGE_API_KEY", "real-key")
	t.Setenv("VOYAGE_MODEL", "voyage-code-3")
	t.Setenv("VOYAGE_API_URL", "https://api.voyageai.com/v1/embeddings")
	for _, e := range snipeEnv() {
		if strings.HasPrefix(e, "VOYAGE_") {
			t.Errorf("snipeEnv passes %s to the binary", e)
		}
	}
}

// End to end: with a key and endpoint in the test process's env, `snipe index`
// run through run() makes no embedding call. Batch mode's URL is fixed to
// api.voyageai.com, so realtime mode stands in for it here; the positive
// control shows the same env does reach the endpoint when passed explicitly.
func TestRun_IndexMakesNoVoyageCall(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "test endpoint", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VOYAGE_API_KEY", "test-key")
	t.Setenv("VOYAGE_API_URL", srv.URL)

	run(t, writeEmbedFixture(t), "index", "--embed-mode=realtime")
	if n := hits.Load(); n != 0 {
		t.Fatalf("run() let snipe index call the Voyage endpoint %d times", n)
	}

	env := append(envWithout(), "KEYRING_DISABLE=1")
	runWithEnv(t, writeEmbedFixture(t), env, "index", "--embed-mode=realtime")
	if hits.Load() == 0 {
		t.Fatal("positive control: an inherited key made no call, so the check above proves nothing")
	}
}
