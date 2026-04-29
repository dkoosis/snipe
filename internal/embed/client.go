package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client handles embedding requests to Voyage AI.
type Client struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

// EmbeddingRequest is the request payload for Voyage AI.
type EmbeddingRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"`
}

// EmbeddingResponse is the response from Voyage AI.
type EmbeddingResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// credentialsPath returns the path to snipe's credentials file.
func credentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "snipe", "credentials")
}

// HasCredentials checks if embedding credentials are available.
// Returns true if SNIPE_VOYAGE_API_KEY env var is set or credentials file exists.
func HasCredentials() bool {
	if os.Getenv("SNIPE_VOYAGE_API_KEY") != "" {
		return true
	}
	path := credentialsPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path from credentialsPath() (user config)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "SNIPE_VOYAGE_API_KEY=")
}

// resolveCredentials reads API key and model from env vars, falling back to the
// credentials file. Returns (apiKey, model, endpoint, error).
func resolveCredentials() (string, string, string, error) {
	apiKey := os.Getenv("SNIPE_VOYAGE_API_KEY")
	model := os.Getenv("VOYAGE_MODEL")
	endpoint := os.Getenv("VOYAGE_API_URL")

	// Fall back to credentials file
	if apiKey == "" {
		creds, err := loadCredentials()
		if err != nil {
			return "", "", "", fmt.Errorf("no API key: set SNIPE_VOYAGE_API_KEY or create ~/.config/snipe/credentials: %w", err)
		}
		apiKey = creds["SNIPE_VOYAGE_API_KEY"]
		if model == "" {
			model = creds["VOYAGE_MODEL"]
		}
		if endpoint == "" {
			endpoint = creds["VOYAGE_API_URL"]
		}
	}

	if apiKey == "" {
		return "", "", "", fmt.Errorf("SNIPE_VOYAGE_API_KEY not set")
	}
	if model == "" {
		model = "voyage-code-3"
	}
	return apiKey, model, endpoint, nil
}

// NewClient creates a new embedding client.
// It reads credentials from ~/.config/snipe/credentials if not provided.
func NewClient() (*Client, error) {
	apiKey, model, endpoint, err := resolveCredentials()
	if err != nil {
		return nil, err
	}
	if endpoint == "" {
		endpoint = "https://api.voyageai.com/v1/embeddings"
	}

	return &Client{
		apiKey:   apiKey,
		model:    model,
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// loadCredentials reads the credentials file.
func loadCredentials() (map[string]string, error) {
	path := credentialsPath()
	if path == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path from credentialsPath() (user config)
	if err != nil {
		return nil, err
	}

	creds := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			creds[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return creds, nil
}

// Embed generates embeddings for the given texts.
// inputType should be "document" for indexing or "query" for search.
func (c *Client) Embed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	req := EmbeddingRequest{
		Input:     texts,
		Model:     c.model,
		InputType: inputType,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var embResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Sort by index to maintain input order
	result := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index >= 0 && d.Index < len(result) {
			result[d.Index] = d.Embedding
		}
	}

	return result, nil
}

// EmbedOne generates embedding for a single text.
func (c *Client) EmbedOne(ctx context.Context, text string, inputType string) ([]float32, error) {
	results, err := c.Embed(ctx, []string{text}, inputType)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// Model returns the model name being used.
func (c *Client) Model() string {
	return c.model
}
