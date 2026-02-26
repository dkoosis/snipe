package embed

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestBatchClient(stateDir string) *BatchClient {
	return &BatchClient{
		apiKey:   "test-key",
		model:    "test-model",
		baseURL:  "http://unused",
		stateDir: stateDir,
	}
}

func TestWriteJSONL(t *testing.T) {
	dir := t.TempDir()
	c := newTestBatchClient(dir)

	symbols := []SymbolText{
		{ID: "sym1", Text: "func Open() error"},
		{ID: "sym2", Text: "func Close() error"},
	}

	path, err := c.WriteJSONL(symbols, dir)
	if err != nil {
		t.Fatalf("WriteJSONL failed: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := splitLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var req BatchRequest
	if err := json.Unmarshal([]byte(lines[0]), &req); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if req.CustomID != "sym1" {
		t.Errorf("custom_id = %q, want %q", req.CustomID, "sym1")
	}
	if req.Body.Input != "func Open() error" {
		t.Errorf("body.input = %q, want %q", req.Body.Input, "func Open() error")
	}
}

func TestParseBatchResults(t *testing.T) {
	c := newTestBatchClient("")

	// Build a valid JSONL batch response
	embBody, _ := json.Marshal(EmbeddingResponse{
		Data: []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
		},
	})

	resp := BatchResponse{
		BatchID:  "batch-1",
		CustomID: "sym1",
		Response: &BatchRespBody{
			StatusCode: 200,
			Body:       embBody,
		},
	}

	line, _ := json.Marshal(resp)
	line = append(line, '\n')

	results := make(map[string][]float32)
	err := c.ParseBatchResults(bytes.NewReader(line), func(id string, emb []float32) error {
		results[id] = emb
		return nil
	})
	if err != nil {
		t.Fatalf("ParseBatchResults failed: %v", err)
	}

	emb, ok := results["sym1"]
	if !ok {
		t.Fatal("missing result for sym1")
	}
	if len(emb) != 3 {
		t.Errorf("embedding length = %d, want 3", len(emb))
	}
	if emb[0] != 0.1 {
		t.Errorf("emb[0] = %f, want 0.1", emb[0])
	}
}

func TestParseBatchResults_SkipsErrors(t *testing.T) {
	c := newTestBatchClient("")

	resp := BatchResponse{
		BatchID:  "batch-1",
		CustomID: "sym1",
		Error:    &BatchError{Code: "rate_limit", Message: "too fast"},
	}

	line, _ := json.Marshal(resp)
	line = append(line, '\n')

	count := 0
	err := c.ParseBatchResults(bytes.NewReader(line), func(id string, emb []float32) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ParseBatchResults failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 results for error response, got %d", count)
	}
}

func TestStatePersistence(t *testing.T) {
	dir := t.TempDir()
	c := newTestBatchClient(dir)

	state := &BatchState{
		BatchID: "batch-123",
		Status:  "in_progress",
		Total:   100,
	}

	if err := c.SaveState(state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := c.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadState returned nil")
	}
	if loaded.BatchID != "batch-123" {
		t.Errorf("BatchID = %q, want %q", loaded.BatchID, "batch-123")
	}
	if loaded.Status != "in_progress" {
		t.Errorf("Status = %q, want %q", loaded.Status, "in_progress")
	}
	if loaded.Total != 100 {
		t.Errorf("Total = %d, want 100", loaded.Total)
	}
}

func TestClearState(t *testing.T) {
	dir := t.TempDir()
	c := newTestBatchClient(dir)

	state := &BatchState{BatchID: "batch-123", Status: "completed"}
	if err := c.SaveState(state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// Verify file exists
	statePath := filepath.Join(dir, "batch_state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state file should exist after SaveState")
	}

	if err := c.ClearState(); err != nil {
		t.Fatalf("ClearState failed: %v", err)
	}

	// Verify file removed
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Error("state file should not exist after ClearState")
	}

	// LoadState should return nil after clear
	loaded, err := c.LoadState()
	if err != nil {
		t.Fatalf("LoadState after clear failed: %v", err)
	}
	if loaded != nil {
		t.Error("LoadState should return nil after ClearState")
	}
}

func TestLoadState_NoFile(t *testing.T) {
	dir := t.TempDir()
	c := newTestBatchClient(dir)

	loaded, err := c.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded != nil {
		t.Error("LoadState should return nil when no state file exists")
	}
}

// splitLines splits on newlines, filtering empty trailing lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
