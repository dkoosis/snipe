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
// path (client.go) and the batch ParseBatchResults path (batch.go): every
// case here must be rejected — error, never a save — because each once
// reached the store as a nil or zero-length embedding (sn-7xp, sn-sts2).
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

// --- Client.Embed (sync path) ---

func TestEmbed_WireContract_SparseResponse(t *testing.T) {
	// 2 texts requested, fixture returns data for only 1 (index 0) — must
	// error rather than return a result with a nil slot at index 1.
	server := serveFixture(t, "voyage_sparse.json")

	got, err := testClient(server.URL).Embed(context.Background(), []string{"a", "b"}, "document")
	if err == nil {
		t.Fatalf("expected error for sparse response, got result: %#v", got)
	}
	if got != nil {
		t.Fatalf("expected nil result on sparse-response error, got: %#v", got)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error does not name the sparse condition: %v", err)
	}
}

func TestEmbed_WireContract_ZeroVectorEmbedding(t *testing.T) {
	// data is present for the requested index, but embedding is []  — a
	// present-but-empty vector must be rejected the same as a missing one,
	// never returned as a zero-length "success".
	server := serveFixture(t, "voyage_zero_vec.json")

	got, err := testClient(server.URL).Embed(context.Background(), []string{"a"}, "document")
	if err == nil {
		t.Fatalf("expected error for zero-length embedding, got result: %#v", got)
	}
	if got != nil {
		t.Fatalf("expected nil result on zero-vector error, got: %#v", got)
	}
}

func TestEmbed_WireContract_UnknownField(t *testing.T) {
	// An extra field on a data item (a Voyage schema addition we don't know
	// about) must fail loudly via DisallowUnknownFields rather than silently
	// parse past it.
	server := serveFixture(t, "voyage_unknown_field.json")

	got, err := testClient(server.URL).Embed(context.Background(), []string{"a"}, "document")
	if err == nil {
		t.Fatalf("expected decode error for unknown field, got result: %#v", got)
	}
	if got != nil {
		t.Fatalf("expected nil result on unknown-field error, got: %#v", got)
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error does not name the decode failure: %v", err)
	}
}

func TestEmbed_WireContract_TruncatedBody(t *testing.T) {
	// Body cut off mid-stream: must surface a decode error, never a
	// zero-value EmbeddingResponse treated as a valid (empty) result.
	server := serveFixture(t, "voyage_truncated.json")

	got, err := testClient(server.URL).Embed(context.Background(), []string{"a"}, "document")
	if err == nil {
		t.Fatalf("expected decode error for truncated body, got result: %#v", got)
	}
	if got != nil {
		t.Fatalf("expected nil result on truncated-body error, got: %#v", got)
	}
}

func TestEmbed_WireContract_MissingIndexRegression(t *testing.T) {
	// data item omits "index" (defaults to Go's zero value, 0). For a 2-text
	// request that leaves index 1 unfilled — the existing nil-slot detection
	// must still catch this rather than silently accept a partial result.
	server := serveFixture(t, "voyage_missing_index.json")

	got, err := testClient(server.URL).Embed(context.Background(), []string{"a", "b"}, "document")
	if err == nil {
		t.Fatalf("expected error for missing index leaving a slot unfilled, got result: %#v", got)
	}
	if got != nil {
		t.Fatalf("expected nil result, got: %#v", got)
	}
}

// --- BatchClient.ParseBatchResults (async batch path) ---

// batchLineWithFixtureBody builds one JSONL line whose response.body is the
// raw bytes of the named fixture, so the same golden fixtures exercise both
// decode paths against the same wire contract.
func batchLineWithFixtureBody(t *testing.T, customID string, fixture string) []byte {
	t.Helper()
	body := wireFixture(t, fixture)
	line, err := json.Marshal(BatchResponse{
		BatchID:  "batch-1",
		CustomID: customID,
		Response: &BatchRespBody{StatusCode: 200, Body: json.RawMessage(body)},
	})
	if err != nil {
		t.Fatalf("marshal batch line: %v", err)
	}
	return append(line, '\n')
}

func TestParseBatchResults_WireContract_EmptyDataItems(t *testing.T) {
	line, err := json.Marshal(BatchResponse{
		BatchID:  "batch-1",
		CustomID: "sym1",
		Response: &BatchRespBody{StatusCode: 200, Body: json.RawMessage(`{"object":"list","data":[],"model":"voyage-code-3","usage":{"total_tokens":0}}`)},
	})
	if err != nil {
		t.Fatalf("marshal batch line: %v", err)
	}

	called := false
	err = testBatchClient("").ParseBatchResults(strings.NewReader(string(line)+"\n"), func(string, []float32) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error for zero data items in a 200 batch row")
	}
	if called {
		t.Fatal("EmbeddingHandler must not be called when data is empty")
	}
}

func TestParseBatchResults_WireContract_ZeroVectorEmbedding(t *testing.T) {
	line := batchLineWithFixtureBody(t, "sym1", "voyage_zero_vec.json")

	called := false
	err := testBatchClient("").ParseBatchResults(strings.NewReader(string(line)), func(string, []float32) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected error for zero-length embedding in a batch row")
	}
	if called {
		t.Fatal("EmbeddingHandler must not be called with a zero-length embedding")
	}
}

func TestParseBatchResults_WireContract_UnknownField(t *testing.T) {
	line := batchLineWithFixtureBody(t, "sym1", "voyage_unknown_field.json")

	called := false
	err := testBatchClient("").ParseBatchResults(strings.NewReader(string(line)), func(string, []float32) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected decode error for unknown field in a batch embedding body")
	}
	if called {
		t.Fatal("EmbeddingHandler must not be called when the body fails to decode")
	}
}

func TestParseBatchResults_WireContract_TruncatedBody(t *testing.T) {
	// The truncated fixture is not valid JSON on its own, so it can't be
	// embedded via json.Marshal(json.RawMessage(...)) — that would compact-
	// validate and fail at construction time instead of at parse time. Splice
	// it into the envelope as raw text, the way a genuinely truncated stream
	// would arrive: the whole line ends up unbalanced JSON.
	line := `{"batch_id":"batch-1","custom_id":"sym1","response":{"status_code":200,"body":` +
		string(wireFixture(t, "voyage_truncated.json")) + `}}` + "\n"

	called := false
	err := testBatchClient("").ParseBatchResults(strings.NewReader(line), func(string, []float32) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected decode error for truncated batch line")
	}
	if called {
		t.Fatal("EmbeddingHandler must not be called when the line fails to decode")
	}
}
