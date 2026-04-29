package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
