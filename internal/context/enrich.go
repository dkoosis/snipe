package context

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// EnrichConfig configures symbol enrichment.
type EnrichConfig struct {
	// DB is the snipe index database
	DB *sql.DB
	// RepoRoot is the absolute path to the repository root
	RepoRoot string
	// Model is the LLM model to use (default "claude-3-5-haiku")
	Model string
	// Force re-enriches even if content hash matches
	Force bool
}

// SymbolInfo contains symbol information for enrichment.
type SymbolInfo struct {
	ID        string
	Name      string
	Kind      string
	Signature string
	Body      string // Truncated to 100 lines
}

// purposePromptTemplate is the prompt template for generating symbol purposes.
const purposePromptTemplate = `Given this Go symbol, provide a 1-line purpose (max 10 words).
Do not start with "This function" or similar. Be terse.

Symbol: %s
Kind: %s
Signature: %s
Body:
` + "```go\n%s\n```" + `

Purpose:`

// EnrichSymbols enriches symbols without doc comments using LLM-generated purposes.
// It returns the count of symbols enriched and any error encountered.
func EnrichSymbols(cfg EnrichConfig) (int, error) {
	if cfg.Model == "" {
		cfg.Model = "claude-3-5-haiku"
	}

	// Ensure symbol_purposes table exists
	if err := ensurePurposesTable(cfg.DB); err != nil {
		return 0, fmt.Errorf("create purposes table: %w", err)
	}

	// Query symbols without doc comments
	symbols, err := getUndocumentedSymbols(cfg.DB, cfg.RepoRoot)
	if err != nil {
		return 0, fmt.Errorf("query undocumented symbols: %w", err)
	}

	enrichedCount := 0
	for _, sym := range symbols {
		// Load symbol body from source file
		body, err := loadSymbolBody(cfg.DB, sym.ID)
		if err != nil {
			// Skip symbols we can't load
			continue
		}
		sym.Body = truncateBody(body, 100)

		// Calculate content hash
		contentHash := calculateContentHash(sym.Signature, sym.Body)

		// Check if we already have a purpose with matching hash
		if !cfg.Force {
			existingHash, err := getStoredHash(cfg.DB, sym.ID)
			if err == nil && existingHash == contentHash {
				// Hash matches, skip enrichment
				continue
			}
		}

		// Generate purpose (placeholder for now)
		purpose, err := generatePurpose(sym)
		if err != nil {
			continue
		}

		// Store the purpose
		if err := storePurpose(cfg.DB, sym.ID, purpose, contentHash, cfg.Model); err != nil {
			continue
		}

		enrichedCount++
	}

	return enrichedCount, nil
}

// ensurePurposesTable creates the symbol_purposes table if it doesn't exist.
func ensurePurposesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS symbol_purposes (
			symbol_id TEXT PRIMARY KEY,
			purpose TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			model TEXT NOT NULL,
			generated_at TEXT NOT NULL,
			FOREIGN KEY (symbol_id) REFERENCES symbols(id)
		)
	`)
	if err != nil {
		return err
	}

	// Create index for content hash lookups
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_purposes_hash ON symbol_purposes(content_hash)`)
	return err
}

// getUndocumentedSymbols queries symbols without doc comments.
func getUndocumentedSymbols(db *sql.DB, repoRoot string) ([]SymbolInfo, error) {
	rows, err := db.Query(`
		SELECT s.id, s.name, s.kind, s.signature
		FROM symbols s
		WHERE s.file_path LIKE ? || '/%'
		  AND s.kind IN ('func', 'method', 'type', 'interface', 'struct')
		  AND (s.doc IS NULL OR s.doc = '')
		  AND s.name GLOB '[A-Z]*'
		ORDER BY s.file_path, s.line_start
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var symbols []SymbolInfo
	for rows.Next() {
		var sym SymbolInfo
		var sig sql.NullString
		if err := rows.Scan(&sym.ID, &sym.Name, &sym.Kind, &sig); err != nil {
			continue
		}
		if sig.Valid {
			sym.Signature = sig.String
		}
		symbols = append(symbols, sym)
	}

	return symbols, rows.Err()
}

// loadSymbolBody loads the source code body for a symbol by ID.
func loadSymbolBody(db *sql.DB, symbolID string) (string, error) {
	var filePath string
	var lineStart, lineEnd int

	err := db.QueryRow(`
		SELECT file_path, line_start, line_end
		FROM symbols
		WHERE id = ?
	`, symbolID).Scan(&filePath, &lineStart, &lineEnd)
	if err != nil {
		return "", fmt.Errorf("query symbol position: %w", err)
	}

	// Read the lines from the file
	file, err := os.Open(filePath) // #nosec G304 -- filePath from trusted index
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum >= lineStart && lineNum <= lineEnd {
			lines = append(lines, scanner.Text())
		}
		if lineNum > lineEnd {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	return strings.Join(lines, "\n"), nil
}

// truncateBody truncates the body to the specified number of lines.
func truncateBody(body string, maxLines int) string {
	lines := strings.Split(body, "\n")
	if len(lines) <= maxLines {
		return body
	}
	return strings.Join(lines[:maxLines], "\n") + "\n// ... truncated"
}

// calculateContentHash computes SHA256 hash of signature and body.
func calculateContentHash(signature, body string) string {
	content := signature + "\n" + body
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// getStoredHash retrieves the stored content hash for a symbol.
func getStoredHash(db *sql.DB, symbolID string) (string, error) {
	var hash string
	err := db.QueryRow(`
		SELECT content_hash FROM symbol_purposes WHERE symbol_id = ?
	`, symbolID).Scan(&hash)
	return hash, err
}

// generatePurpose generates a 1-line purpose for a symbol.
// NOTE: This is a placeholder. Actual LLM integration will be added later.
//
//nolint:unparam // error return reserved for future LLM integration
func generatePurpose(sym SymbolInfo) (string, error) {
	// TODO: Implement actual LLM API call here.
	// The call should:
	// 1. Format the prompt using purposePromptTemplate with sym.Name, sym.Kind, sym.Signature, sym.Body
	// 2. Call the LLM API (e.g., Anthropic Claude API)
	// 3. Parse the response to extract the purpose line
	// 4. Return the purpose (max 10 words)
	//
	// Example prompt that would be sent:
	// prompt := fmt.Sprintf(purposePromptTemplate, sym.Name, sym.Kind, sym.Signature, sym.Body)
	//
	// For now, return a placeholder indicating enrichment is pending.
	_ = fmt.Sprintf(purposePromptTemplate, sym.Name, sym.Kind, sym.Signature, sym.Body)
	return "[LLM enrichment pending]", nil
}

// storePurpose stores the generated purpose in the database.
func storePurpose(db *sql.DB, symbolID, purpose, contentHash, model string) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO symbol_purposes (symbol_id, purpose, content_hash, model, generated_at)
		VALUES (?, ?, ?, ?, ?)
	`, symbolID, purpose, contentHash, model, time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetPurpose retrieves the stored purpose for a symbol.
func GetPurpose(db *sql.DB, symbolID string) (string, error) {
	var purpose string
	err := db.QueryRow(`
		SELECT purpose FROM symbol_purposes WHERE symbol_id = ?
	`, symbolID).Scan(&purpose)
	return purpose, err
}

// PruneOrphanedPurposes removes purposes for symbols that no longer exist.
func PruneOrphanedPurposes(db *sql.DB) (int64, error) {
	result, err := db.Exec(`
		DELETE FROM symbol_purposes
		WHERE symbol_id NOT IN (SELECT id FROM symbols)
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
