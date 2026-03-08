package embed

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testBatchClient(stateDir string) *BatchClient {
	return &BatchClient{
		apiKey:   "test-key",
		model:    "test-model",
		baseURL:  "http://unused",
		stateDir: stateDir,
	}
}

func TestBatchWriteJSONLAndParseResultsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := testBatchClient(dir)

	symbols := []SymbolText{
		{ID: "sym1", Text: "func Open() error"},
		{ID: "sym2", Text: "func Close() error"},
	}

	path, err := c.WriteJSONL(symbols, dir)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var reqs []BatchRequest
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var req BatchRequest
		require.NoError(t, json.Unmarshal(line, &req))
		reqs = append(reqs, req)
	}

	require.Len(t, reqs, 2)
	require.Equal(t, "sym1", reqs[0].CustomID)
	require.Equal(t, "func Open() error", reqs[0].Body.Input)
	require.Equal(t, "sym2", reqs[1].CustomID)
	require.Equal(t, "func Close() error", reqs[1].Body.Input)
}

func TestBatchParseResultsBehavior(t *testing.T) {
	c := testBatchClient("")

	embedBody, err := json.Marshal(EmbeddingResponse{
		Data: []struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0}},
	})
	require.NoError(t, err)

	line := func(resp BatchResponse) []byte {
		b, err := json.Marshal(resp)
		require.NoError(t, err)
		return append(b, '\n')
	}

	t.Run("emits only successful embeddings", func(t *testing.T) {
		stream := bytes.Join([][]byte{
			line(BatchResponse{BatchID: "1", CustomID: "ok", Response: &BatchRespBody{StatusCode: 200, Body: embedBody}}),
			line(BatchResponse{BatchID: "1", CustomID: "skip_error", Error: &BatchError{Code: "rate_limit", Message: "too fast"}}),
			line(BatchResponse{BatchID: "1", CustomID: "skip_status", Response: &BatchRespBody{StatusCode: 500, Body: embedBody}}),
			[]byte("\n"),
		}, nil)

		results := map[string][]float32{}
		err := c.ParseBatchResults(bytes.NewReader(stream), func(id string, emb []float32) error {
			results[id] = emb
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, map[string][]float32{"ok": {0.1, 0.2, 0.3}}, results)
	})

	t.Run("returns callback error", func(t *testing.T) {
		stream := line(BatchResponse{BatchID: "1", CustomID: "ok", Response: &BatchRespBody{StatusCode: 200, Body: embedBody}})
		err := c.ParseBatchResults(bytes.NewReader(stream), func(string, []float32) error {
			return errors.New("boom")
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "process embedding for ok")
	})

	t.Run("returns parse errors for invalid lines", func(t *testing.T) {
		err := c.ParseBatchResults(bytes.NewReader([]byte("{not json}\n")), func(string, []float32) error { return nil })
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse line")
	})

	t.Run("returns parse errors for invalid embedding body", func(t *testing.T) {
		line := `{"batch_id":"1","custom_id":"bad","response":{"status_code":200,"body":"not-json"}}` + "\n"
		err := c.ParseBatchResults(bytes.NewReader([]byte(line)), func(string, []float32) error { return nil })
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse embedding result for bad")
	})
}

func TestBatchStatePersistence(t *testing.T) {
	dir := t.TempDir()
	c := testBatchClient(dir)

	state := &BatchState{BatchID: "batch-123", Status: "in_progress", Total: 100}
	require.NoError(t, c.SaveState(state))

	loaded, err := c.LoadState()
	require.NoError(t, err)
	require.Equal(t, state.BatchID, loaded.BatchID)
	require.Equal(t, state.Status, loaded.Status)
	require.Equal(t, state.Total, loaded.Total)

	require.NoError(t, c.ClearState())
	loaded, err = c.LoadState()
	require.NoError(t, err)
	require.Nil(t, loaded)

	_, err = os.Stat(filepath.Join(dir, "batch_state.json"))
	require.True(t, os.IsNotExist(err))
}

func TestBatchLoadStateWithoutFile(t *testing.T) {
	c := testBatchClient(t.TempDir())
	state, err := c.LoadState()
	require.NoError(t, err)
	require.Nil(t, state)
}

func FuzzParseBatchResults(f *testing.F) {
	f.Add("\n")
	f.Add(`{"batch_id":"1","custom_id":"ok","response":{"status_code":200,"body":{"data":[{"embedding":[0.1],"index":0}]}}}` + "\n")
	f.Add(`{"batch_id":"1","custom_id":"bad","response":{"status_code":200,"body":{"data":` + "\n")

	c := testBatchClient("")
	f.Fuzz(func(t *testing.T, input string) {
		_ = c.ParseBatchResults(bytes.NewReader([]byte(input)), func(string, []float32) error { return nil })
	})
}
