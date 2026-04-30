package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type failingTransport struct{ err error }

func (f *failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, f.err
}

func testBatchClient(stateDir string) *BatchClient {
	return &BatchClient{
		apiKey:   "test-key",
		model:    "test-model",
		baseURL:  "http://unused",
		stateDir: stateDir,
	}
}

func TestWriteJSONL_WritesExpectedRequests(t *testing.T) {
	dir := t.TempDir()
	path, err := testBatchClient(dir).WriteJSONL([]SymbolText{
		{ID: "sym1", Text: "func Open() error"},
		{ID: "sym2", Text: "func Close() error"},
	}, dir)
	if err != nil {
		t.Fatalf("WriteJSONL failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}

	var got []BatchRequest
	for i, line := range lines {
		var req BatchRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}
		got = append(got, req)
	}

	if got[0].CustomID != "sym1" || got[0].Body.Input != "func Open() error" {
		t.Fatalf("unexpected first request: %+v", got[0])
	}
	if got[1].CustomID != "sym2" || got[1].Body.Input != "func Close() error" {
		t.Fatalf("unexpected second request: %+v", got[1])
	}
}

func TestParseBatchResults_Behavior(t *testing.T) {
	embBody, _ := json.Marshal(EmbeddingResponse{
		Data: []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
		},
	})

	lines := []BatchResponse{
		{
			BatchID:  "batch-1",
			CustomID: "ok",
			Response: &BatchRespBody{StatusCode: 200, Body: embBody},
		},
		{
			BatchID:  "batch-1",
			CustomID: "skip-error",
			Error:    &BatchError{Code: "rate_limit", Message: "too fast"},
		},
		{
			BatchID:  "batch-1",
			CustomID: "skip-status",
			Response: &BatchRespBody{StatusCode: 429, Body: embBody},
		},
	}

	var payload bytes.Buffer
	for _, resp := range lines {
		line, _ := json.Marshal(resp)
		payload.Write(line)
		payload.WriteByte('\n')
	}

	results := map[string][]float32{}
	err := testBatchClient("").ParseBatchResults(&payload, func(id string, emb []float32) error {
		results[id] = emb
		return nil
	})
	if err != nil {
		t.Fatalf("ParseBatchResults failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("result count = %d, want 1", len(results))
	}
	if len(results["ok"]) != 3 || results["ok"][0] != 0.1 {
		t.Fatalf("unexpected parsed embedding: %#v", results)
	}
}

func TestParseBatchResults_PropagatesCallbackError(t *testing.T) {
	embBody, _ := json.Marshal(EmbeddingResponse{
		Data: []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{{Embedding: []float32{1}, Index: 0}},
	})
	line, _ := json.Marshal(BatchResponse{
		BatchID:  "batch-1",
		CustomID: "sym1",
		Response: &BatchRespBody{StatusCode: 200, Body: embBody},
	})

	wantErr := errors.New("stop")
	err := testBatchClient("").ParseBatchResults(bytes.NewReader(append(line, '\n')), func(string, []float32) error {
		return wantErr
	})
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("expected callback error, got %v", err)
	}
}

func TestBatchState_PersistenceLifecycle(t *testing.T) {
	dir := t.TempDir()
	c := testBatchClient(dir)

	state := &BatchState{
		BatchID:          "batch-123",
		Status:           "in_progress",
		Total:            100,
		IndexFingerprint: "abc123",
	}
	if err := c.SaveState(state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := c.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil || loaded.BatchID != state.BatchID || loaded.Status != state.Status || loaded.Total != state.Total || loaded.IndexFingerprint != state.IndexFingerprint {
		t.Fatalf("loaded state mismatch: %#v", loaded)
	}

	if err := c.ClearState(); err != nil {
		t.Fatalf("ClearState failed: %v", err)
	}

	again, err := c.LoadState()
	if err != nil {
		t.Fatalf("LoadState after clear failed: %v", err)
	}
	if again != nil {
		t.Fatalf("expected no state after clear, got %#v", again)
	}
	if _, err := os.Stat(filepath.Join(dir, "batch_state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state file should be removed, stat err: %v", err)
	}
}

func TestLoadState_NoFile(t *testing.T) {
	loaded, err := testBatchClient(t.TempDir()).LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected nil when state file does not exist, got %#v", loaded)
	}
}

func TestMatchesFingerprint(t *testing.T) {
	tests := []struct {
		name        string
		state       *BatchState
		fingerprint string
		want        bool
	}{
		{name: "matching", state: &BatchState{IndexFingerprint: "abc123"}, fingerprint: "abc123", want: true},
		{name: "mismatched", state: &BatchState{IndexFingerprint: "abc123"}, fingerprint: "def456", want: false},
		{name: "empty state fingerprint", state: &BatchState{}, fingerprint: "abc123", want: false},
		{name: "empty current fingerprint", state: &BatchState{IndexFingerprint: "abc123"}, fingerprint: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.MatchesFingerprint(tt.fingerprint); got != tt.want {
				t.Fatalf("MatchesFingerprint(%q) = %v, want %v", tt.fingerprint, got, tt.want)
			}
		})
	}
}

func FuzzParseBatchResults(f *testing.F) {
	f.Add("{\"batch_id\":\"b\",\"custom_id\":\"x\",\"response\":{\"status_code\":200,\"body\":{\"data\":[{\"embedding\":[0.1],\"index\":0}]}}}\n")
	f.Add("not json\n")
	f.Add("\n")

	f.Fuzz(func(t *testing.T, input string) {
		_ = testBatchClient("").ParseBatchResults(strings.NewReader(input), func(string, []float32) error {
			return nil
		})
	})
}

func TestUploadFile_NoGoroutineLeakOnTransportError(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "in.jsonl")
	// Large enough that the producer would block on the pipe if the consumer
	// abandons the request. 1MB > pipe buffer.
	payload := bytes.Repeat([]byte("{\"custom_id\":\"x\",\"body\":{\"input\":\"y\"}}\n"), 32*1024)
	if err := os.WriteFile(jsonl, payload, 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	c := testBatchClient(dir)
	c.client = &http.Client{Transport: &failingTransport{err: errors.New("boom")}}

	// Settle pre-existing goroutines (test runner spawns some lazily).
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		if _, err := c.UploadFile(context.Background(), jsonl); err == nil {
			t.Fatalf("expected error on iteration %d", i)
		}
	}

	// Allow finalizers/scheduler to settle, then assert no growth.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > before+1 {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}
