package embed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func testClient(endpoint string) *Client {
	return &Client{
		apiKey:   "test-key",
		model:    "test-model",
		endpoint: endpoint,
		client:   http.DefaultClient,
	}
}

func TestClientEmbedBehavior(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		inputType string
		handler   http.HandlerFunc
		want      [][]float32
		wantErr   string
	}{
		{
			name:      "returns embeddings in input order by index",
			input:     []string{"hello", "world"},
			inputType: "document",
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

				resp := EmbeddingResponse{
					Object: "list",
					Data: []struct {
						Object    string    `json:"object"`
						Embedding []float32 `json:"embedding"`
						Index     int       `json:"index"`
					}{
						{Object: "embedding", Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
						{Object: "embedding", Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
					},
					Model: "test-model",
				}
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(resp))
			},
			want: [][]float32{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
		},
		{
			name:      "propagates non-200 api errors",
			input:     []string{"hello"},
			inputType: "document",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
			},
			wantErr: "API error 401",
		},
		{
			name:      "errors on invalid json response",
			input:     []string{"hello"},
			inputType: "query",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":`))
			},
			wantErr: "decode response",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			c := testClient(server.URL)
			got, err := c.Embed(tc.input, tc.inputType)

			if tc.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClientEmbedEmptyInputSkipsNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server should not be called for empty input")
	}))
	defer server.Close()

	c := testClient(server.URL)
	results, err := c.Embed(nil, "document")
	require.NoError(t, err)
	require.Nil(t, results)
}

func TestClientEmbedOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := EmbeddingResponse{
			Data: []struct {
				Object    string    `json:"object"`
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer server.Close()

	c := testClient(server.URL)
	got, err := c.EmbedOne("hello", "query")
	require.NoError(t, err)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, got)
}

func TestClientEmbedOnePropagatesEmbedErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
	}))
	defer server.Close()

	c := testClient(server.URL)
	_, err := c.EmbedOne("hello", "query")
	require.Error(t, err)
	require.Contains(t, err.Error(), "API error 401")
}
