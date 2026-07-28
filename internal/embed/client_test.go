package embed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dkoosis/keyring"
)

func testClient(endpoint string) *Client {
	return &Client{
		apiKey:   "test-key",
		model:    "test-model",
		endpoint: endpoint,
		client:   http.DefaultClient,
	}
}

func TestEmbed_SendsRequestAndReturnsEmbeddingsInInputOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}

		var req EmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" || req.InputType != "document" || len(req.Input) != 2 || req.Input[0] != "hello" || req.Input[1] != "world" {
			t.Fatalf("unexpected request body: %+v", req)
		}

		_ = json.NewEncoder(w).Encode(EmbeddingResponse{
			Data: []struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{9, 9}, Index: 1},
				{Embedding: []float32{1, 2}, Index: 0},
			},
		})
	}))
	defer server.Close()

	got, err := testClient(server.URL).Embed(context.Background(), []string{"hello", "world"}, "document")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(got) != 2 || len(got[0]) != 2 || len(got[1]) != 2 {
		t.Fatalf("unexpected embedding shape: %#v", got)
	}
	if got[0][0] != 1 || got[0][1] != 2 || got[1][0] != 9 || got[1][1] != 9 {
		t.Fatalf("embeddings not in input order: %#v", got)
	}
}

func TestEmbed_ErrorBehavior(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		setup    func(*testing.T) *Client
		assertFn func(*testing.T, [][]float32, error)
	}{
		{
			name:  "empty input returns nil result without request",
			input: nil,
			setup: func(*testing.T) *Client {
				return testClient("http://unused")
			},
			assertFn: func(t *testing.T, got [][]float32, err error) {
				if err != nil {
					t.Fatalf("Embed error: %v", err)
				}
				if got != nil {
					t.Fatalf("expected nil result, got %#v", got)
				}
			},
		},
		{
			name:  "non-200 response includes status code",
			input: []string{"hello"},
			setup: func(t *testing.T) *Client {
				t.Helper()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
				}))
				t.Cleanup(server.Close)
				return testClient(server.URL)
			},
			assertFn: func(t *testing.T, _ [][]float32, err error) {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), "401") {
					t.Fatalf("error does not include status code: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setup(t)
			got, err := client.Embed(context.Background(), tt.input, "document")
			tt.assertFn(t, got, err)
		})
	}
}

func TestEmbed_RespectsContextCancellation(t *testing.T) {
	// Server blocks long enough to outlast the client ctx; cancellation must
	// abort the in-flight request without waiting for the server response.
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(stop) })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := testClient(server.URL).Embed(ctx, []string{"x"}, "document")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Embed did not abort promptly: took %v", elapsed)
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected context/deadline error, got: %v", err)
	}
}

// TestCredentials_IgnoresPlaintextFile guards the sn-nduv removal: the
// deprecated ~/.config/snipe/credentials fallback is gone, so a planted file
// must never satisfy HasCredentials or resolveCredentials. Keychain is disabled
// and the env var cleared, leaving the file as the only possible source — and
// it must be ignored.
func TestCredentials_IgnoresPlaintextFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KEYRING_DISABLE", "1")
	t.Setenv("SNIPE_VOYAGE_API_KEY", "")

	credDir := filepath.Join(home, ".config", "snipe")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "credentials"), []byte("SNIPE_VOYAGE_API_KEY=planted-key\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	if HasCredentials() {
		t.Error("HasCredentials returned true from a plaintext file — the file fallback must be gone")
	}
	if _, _, _, err := resolveCredentials(); err == nil {
		t.Error("resolveCredentials succeeded from a plaintext file — the file fallback must be gone")
	}
}

// TestCredentials_EnvFirst locks the env-first ordering (AXI #6: never prompt).
// When SNIPE_VOYAGE_API_KEY is set, resolveCredentials returns it and
// HasCredentials reports true WITHOUT consulting the keychain — so an
// env-provisioned process (agent, CI, orca) never execs `security` and can never
// trip an OS unlock/allow dialog. KEYRING_DISABLE is left UNSET to prove the env
// var alone short-circuits even with a keychain backend available; that
// short-circuit is also what keeps this test hermetic across platforms.
func TestCredentials_EnvFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KEYRING_DISABLE", "")
	t.Setenv("SNIPE_VOYAGE_API_KEY", "env-provided-key")

	// The no-prompt contract is not "env wins" but "the keychain is never
	// opened": opening it is what execs `security` and can hang a headless
	// agent. Fail if credStore is touched at all while the env key is set.
	orig := credStore
	t.Cleanup(func() { credStore = orig })
	credStore = func() (*keyring.Store, error) {
		t.Error("credStore opened despite SNIPE_VOYAGE_API_KEY being set — this can exec `security` and hang headless agents")
		return nil, errors.New("credStore must not be opened when the env key is set")
	}

	if !HasCredentials() {
		t.Error("HasCredentials should report true when the env var is set")
	}
	key, _, _, err := resolveCredentials()
	if err != nil {
		t.Fatalf("resolveCredentials: %v", err)
	}
	if key != "env-provided-key" {
		t.Errorf("apiKey = %q, want the env-provided key (env must win over keychain)", key)
	}
}

// TestLiveProbe covers the doctor --probe path: a real one-shot request that
// proves the resolved key works (2xx) or surfaces the failure (bad key, 4xx).
// NewClient reads VOYAGE_API_URL for the endpoint, so the httptest server stands
// in for Voyage. KEYRING_DISABLE keeps the probe hermetic — env key only.
func TestLiveProbe(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "valid key returns nil",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(EmbeddingResponse{
					Data: []struct {
						Object    string    `json:"object"`
						Embedding []float32 `json:"embedding"`
						Index     int       `json:"index"`
					}{{Embedding: []float32{0.1}, Index: 0}},
				})
			},
			wantErr: false,
		},
		{
			name: "unauthorized key returns error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			t.Setenv("KEYRING_DISABLE", "1")
			t.Setenv("SNIPE_VOYAGE_API_KEY", "test-key")
			t.Setenv("VOYAGE_API_URL", server.URL)

			err := LiveProbe(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("LiveProbe: %v", err)
			}
		})
	}
}

func TestEmbedOne_ReturnsFirstEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(EmbeddingResponse{
			Data: []struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		})
	}))
	defer server.Close()

	got, err := testClient(server.URL).EmbedOne(context.Background(), "hello", "query")
	if err != nil {
		t.Fatalf("EmbedOne failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("embedding length = %d, want 3", len(got))
	}
}
