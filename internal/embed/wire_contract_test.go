package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wireFixture reads a raw Voyage response body from testdata/. These are the
// golden fixtures for the response-shape contract shared by the sync Embed
// path (client.go) and the batch ParseBatchResults path (batch.go): a
// sparse or zero-length embedding must be rejected — error, never a save —
// because each once reached the store as a nil or zero-length embedding
// (sn-7xp, sn-sts2). An unknown field is the deliberate counter-case: Voyage
// is an external API, so additive drift stays tolerated.
func wireFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func serveFixture(t *testing.T, name string) *httptest.Server {
	t.Helper()
	body := wireFixture(t, name)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestEmbed_WireContract drives Client.Embed against each golden fixture.
func TestEmbed_WireContract(t *testing.T) {
	cases := []struct {
		name string
		// fixture is the raw Voyage 200 body the server returns.
		fixture string
		// texts is the request — its length sets how many slots must fill.
		texts []string
		// wantErrContains empty means the response must be accepted.
		wantErrContains string
		// wantEmbeddings is checked only on the accepted cases.
		wantEmbeddings [][]float32
		why            string
	}{
		{
			name:            "sparse response leaves a slot unfilled",
			fixture:         "voyage_sparse.json",
			texts:           []string{"a", "b"},
			wantErrContains: "missing",
			why:             "2 texts requested, data for index 0 only — must error, not return a nil slot at index 1",
		},
		{
			name:            "present but zero-length embedding",
			fixture:         "voyage_zero_vec.json",
			texts:           []string{"a"},
			wantErrContains: "missing or empty",
			why:             "an empty vector must be rejected the same as a missing one, never returned as a zero-length success",
		},
		{
			name:            "truncated body",
			fixture:         "voyage_truncated.json",
			texts:           []string{"a"},
			wantErrContains: "decode response",
			why:             "a body cut off mid-stream must surface a decode error, never a zero-value response read as a valid empty result",
		},
		{
			name:            "missing index defaults to 0 and leaves a slot unfilled",
			fixture:         "voyage_missing_index.json",
			texts:           []string{"a", "b"},
			wantErrContains: "missing",
			why:             "an omitted index zero-values to 0, so index 1 never fills — the nil-slot check must still catch it",
		},
		{
			name:           "unknown field is tolerated",
			fixture:        "voyage_unknown_field.json",
			texts:          []string{"a"},
			wantEmbeddings: [][]float32{{0.1, 0.2}},
			why:            "Voyage may add response metadata; strict decoding would turn a harmless addition into a total embedding outage",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := serveFixture(t, tc.fixture)

			got, err := testClient(server.URL).Embed(context.Background(), tc.texts, "document")

			if tc.wantErrContains == "" {
				if err != nil {
					t.Fatalf("expected acceptance (%s), got error: %v", tc.why, err)
				}
				if len(got) != len(tc.wantEmbeddings) {
					t.Fatalf("got %d embeddings, want %d", len(got), len(tc.wantEmbeddings))
				}
				for i, want := range tc.wantEmbeddings {
					if len(got[i]) != len(want) {
						t.Fatalf("embedding %d: got %#v, want %#v", i, got[i], want)
					}
					for j, v := range want {
						if got[i][j] != v {
							t.Fatalf("embedding %d: got %#v, want %#v", i, got[i], want)
						}
					}
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error (%s), got result: %#v", tc.why, got)
			}
			if got != nil {
				t.Fatalf("expected nil result on error, got: %#v", got)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("error does not name the condition %q: %v", tc.wantErrContains, err)
			}
		})
	}
}

// batchLine builds one JSONL line whose response.body is the given raw bytes,
// so the same golden fixtures exercise both decode paths.
func batchLine(t *testing.T, customID string, body []byte) string {
	t.Helper()
	line, err := json.Marshal(BatchResponse{
		BatchID:  "batch-1",
		CustomID: customID,
		Response: &BatchRespBody{StatusCode: 200, Body: json.RawMessage(body)},
	})
	if err != nil {
		t.Fatalf("marshal batch line: %v", err)
	}
	return string(line) + "\n"
}

// TestParseBatchResults_WireContract holds the batch path to the same
// contract as the sync path above.
func TestParseBatchResults_WireContract(t *testing.T) {
	cases := []struct {
		name string
		// line is built per-case because the truncated case cannot go
		// through json.Marshal — it would fail at construction rather than
		// at parse time.
		line func(t *testing.T) string
		// wantHandled empty means the row must be rejected before fn runs.
		wantHandled []float32
		why         string
	}{
		{
			name: "zero data items",
			line: func(t *testing.T) string {
				return batchLine(t, "sym1", []byte(`{"object":"list","data":[],"model":"voyage-code-3","usage":{"total_tokens":0}}`))
			},
			why: "a 200 row carrying no data item must error rather than silently skip the symbol",
		},
		{
			name: "present but zero-length embedding",
			line: func(t *testing.T) string {
				return batchLine(t, "sym1", wireFixture(t, "voyage_zero_vec.json"))
			},
			why: "an empty vector must never reach the handler, which would save it",
		},
		{
			name: "truncated body",
			line: func(t *testing.T) string {
				return `{"batch_id":"batch-1","custom_id":"sym1","response":{"status_code":200,"body":` +
					string(wireFixture(t, "voyage_truncated.json")) + `}}` + "\n"
			},
			why: "an unbalanced line must fail to decode rather than yield a zero-value row",
		},
		{
			name: "unknown field is tolerated",
			line: func(t *testing.T) string {
				return batchLine(t, "sym1", wireFixture(t, "voyage_unknown_field.json"))
			},
			wantHandled: []float32{0.1, 0.2},
			why:         "additive Voyage drift must not block importing a completed batch",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var handled []float32
			called := false
			err := testBatchClient("").ParseBatchResults(strings.NewReader(tc.line(t)), func(_ string, v []float32) error {
				called = true
				handled = v
				return nil
			})

			if tc.wantHandled != nil {
				if err != nil {
					t.Fatalf("expected acceptance (%s), got error: %v", tc.why, err)
				}
				if !called {
					t.Fatalf("EmbeddingHandler was not called (%s)", tc.why)
				}
				if len(handled) != len(tc.wantHandled) {
					t.Fatalf("handler got %#v, want %#v", handled, tc.wantHandled)
				}
				for i, want := range tc.wantHandled {
					if handled[i] != want {
						t.Fatalf("handler got %#v, want %#v", handled, tc.wantHandled)
					}
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error (%s)", tc.why)
			}
			if called {
				t.Fatalf("EmbeddingHandler must not be called (%s)", tc.why)
			}
		})
	}
}
