package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(endpoint string) *Client {
	return &Client{
		apiKey:   "test-key",
		model:    "test-model",
		endpoint: endpoint,
		client:   http.DefaultClient,
	}
}

func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		resp := EmbeddingResponse{
			Object: "list",
			Data: []struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Object: "embedding", Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Object: "embedding", Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
			},
			Model: "test-model",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server.URL)
	results, err := c.Embed([]string{"hello", "world"}, "document")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0][0] != 0.1 {
		t.Errorf("results[0][0] = %f, want 0.1", results[0][0])
	}
	if results[1][0] != 0.4 {
		t.Errorf("results[1][0] = %f, want 0.4", results[1][0])
	}
}

func TestEmbedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_api_key"}`))
	}))
	defer server.Close()

	c := newTestClient(server.URL)
	_, err := c.Embed([]string{"hello"}, "document")
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if !contains(err.Error(), "401") {
		t.Errorf("error should mention 401, got: %s", err.Error())
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	c := newTestClient("http://unused")
	results, err := c.Embed(nil, "document")
	if err != nil {
		t.Fatalf("Embed with empty input should not error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty input, got %v", results)
	}
}

func TestEmbedOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbeddingResponse{
			Data: []struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	c := newTestClient(server.URL)
	result, err := c.EmbedOne("hello", "query")
	if err != nil {
		t.Fatalf("EmbedOne failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(result))
	}
}

func TestClientModel(t *testing.T) {
	c := newTestClient("http://unused")
	if c.Model() != "test-model" {
		t.Errorf("Model() = %q, want %q", c.Model(), "test-model")
	}
}

func TestClientDimensions(t *testing.T) {
	c := newTestClient("http://unused")
	if c.Dimensions() != 1024 {
		t.Errorf("Dimensions() = %d, want 1024", c.Dimensions())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
