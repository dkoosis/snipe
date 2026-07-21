package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dkoosis/keyring"
)

// keyringService is the macOS keychain namespace for this repo's CLI binary
// (convention: service = app name, account = provider). The Voyage API key
// lives at service "snipe", account "voyage".
const (
	keyringService = "snipe"
	keyringAccount = "voyage"
)

// credStore returns the keychain store for snipe's secrets. keyring.New only
// fails on an empty service name, so the error is effectively unreachable
// with the literal above. A var, not a func, so tests can assert it is never
// opened when the env key short-circuits the keychain path.
var credStore = func() (*keyring.Store, error) {
	return keyring.New(keyringService)
}

// probeMu guards errLastProbe and warnedAccounts.
var (
	probeMu        sync.Mutex
	errLastProbe   error
	warnedAccounts = map[string]bool{}
)

// LastProbeErr returns the keychain error recorded by the most recent
// credential probe (HasCredentials or resolveCredentials). Non-nil means the
// keychain item was unreadable — locked, denied, or timed out
// (keyring.ErrUnreadable) — so callers like doctor can report "keychain
// locked — using env/file if set" instead of a false "credentials available".
func LastProbeErr() error {
	probeMu.Lock()
	defer probeMu.Unlock()
	return errLastProbe
}

// recordProbe stores the outcome of a keychain probe for LastProbeErr.
func recordProbe(err error) {
	probeMu.Lock()
	defer probeMu.Unlock()
	errLastProbe = err
}

// warnUnreadableOnce logs one prominent warning per account per process when
// the keychain is unreadable and we downgrade to the env var. The keyring
// contract forbids a SILENT downgrade; for a consumer that can run headless or
// in the background (snipe index), a LOUD one-time downgrade is the correct
// behavior — name the locked keychain, the account, and the fallback, then
// carry on.
func warnUnreadableOnce(account string, err error) {
	probeMu.Lock()
	defer probeMu.Unlock()
	if warnedAccounts[account] {
		return
	}
	warnedAccounts[account] = true
	fmt.Fprintf(os.Stderr,
		"WARNING: keychain item %s/%s is unreadable (keychain locked or access denied): %v — falling back to SNIPE_VOYAGE_API_KEY\n",
		keyringService, account, err)
}

// envAPIKey is the single os.Getenv call site for SNIPE_VOYAGE_API_KEY in
// this package (the blackbox literals test counts exactly one).
func envAPIKey() string { return os.Getenv("SNIPE_VOYAGE_API_KEY") }

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

// HasCredentials checks if embedding credentials are available.
// Returns true if the SNIPE_VOYAGE_API_KEY env var is set or the keychain holds
// a voyage key. The env var is checked FIRST so an env-provisioned process never
// touches the keychain (AXI #6: never prompt — a `security` read can pop an OS
// unlock/allow dialog). An unreadable keychain (locked/denied — ErrUnreadable)
// is NOT treated as "credentials available": the locked state is recorded in
// LastProbeErr so doctor can name it, keeping this probe in agreement with
// resolveCredentials.
func HasCredentials() bool {
	recordProbe(nil)
	if envAPIKey() != "" {
		return true
	}
	if store, err := credStore(); err == nil {
		ok, hasErr := store.Has(keyringAccount)
		if hasErr == nil && ok {
			return true
		}
		if hasErr != nil && errors.Is(hasErr, keyring.ErrUnreadable) {
			// Locked or denied keychain — not confirmation of absence. Record it
			// so doctor can name the state.
			recordProbe(hasErr)
		}
	}
	return false
}

// resolveCredentials reads the API key env-first (SNIPE_VOYAGE_API_KEY), then
// the keychain. Env-first means an env-provisioned process — agents, CI, orca —
// never execs `security`, so it can never trigger an OS keychain prompt (AXI #6:
// never prompt for interactive input). The keychain is consulted only as a
// fallback for humans who stored the key there. Model and endpoint are config,
// not secrets — plain env only. Returns (apiKey, model, endpoint, error).
func resolveCredentials() (string, string, string, error) {
	apiKey := envAPIKey()
	if apiKey == "" {
		// Env absent — fall back to the keychain. This is the only path that
		// execs `security`; skipping it whenever the env var is set is what
		// keeps automated invocations prompt-free.
		store, err := credStore()
		if err != nil {
			return "", "", "", fmt.Errorf("opening keyring store: %w", err)
		}
		key, keyErr := store.Get(keyringAccount)
		switch {
		case keyErr == nil:
			apiKey = key
		case errors.Is(keyErr, keyring.ErrNotFound), errors.Is(keyErr, keyring.ErrUnsupported):
			// Confirmed absent, or no keychain backend (incl. KEYRING_DISABLE):
			// nothing left to try.
		case errors.Is(keyErr, keyring.ErrUnreadable):
			// Locked or denied keychain. snipe index can run headless or in the
			// background, so a hard error here would be wrong — but a silent
			// downgrade violates the keyring contract. Warn LOUDLY once per
			// process; embeddings then switch off via the caller's Warning path.
			recordProbe(keyErr)
			warnUnreadableOnce(keyringAccount, keyErr)
		default:
			return "", "", "", fmt.Errorf("reading voyage API key from keychain: %w", keyErr)
		}
	}
	model := os.Getenv("VOYAGE_MODEL")
	endpoint := os.Getenv("VOYAGE_API_URL")

	if apiKey == "" {
		return "", "", "", fmt.Errorf("no API key: set SNIPE_VOYAGE_API_KEY or store one in the keychain (security add-generic-password -U -s snipe -a voyage -w KEY)")
	}
	if model == "" {
		model = "voyage-code-3"
	}
	return apiKey, model, endpoint, nil
}

// NewClient creates a new embedding client.
// It reads the API key env-first (SNIPE_VOYAGE_API_KEY), falling back to the keychain.
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

	result := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index >= 0 && d.Index < len(result) {
			result[d.Index] = d.Embedding
		}
	}

	// Detect sparse responses — Voyage can return fewer items than requested
	// with no top-level error, which would silently write nil embeddings.
	missing := 0
	for _, v := range result {
		if v == nil {
			missing++
		}
	}
	if missing > 0 {
		return nil, fmt.Errorf("embed: %d of %d responses missing from API", missing, len(texts))
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
