package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status and statistics",
	Long: `Shows the current state of the snipe index including:
- Whether the index is fresh, stale, or missing
- Git commit at time of indexing
- Symbol, reference, and call edge counts

Examples:
  snipe status           # Show index status
  snipe status --json    # Output as JSON`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

// StatusResponse is the JSON response for status command.
type StatusResponse struct {
	State       output.IndexState `json:"state"`
	Commit      string            `json:"commit,omitempty"`
	IndexedAt   string            `json:"indexed_at,omitempty"`
	Symbols     int               `json:"symbols"`
	Refs        int               `json:"refs"`
	Calls       int               `json:"calls"`
	Fingerprint string            `json:"fingerprint,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	human, compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, human, compact)

	// Find repo root
	dir := findProjectRoot(".")
	if dir == "" {
		return w.WriteError("status", &output.Error{
			Code:    output.ErrInternal,
			Message: "not in a git repository",
		})
	}

	// Check if index exists
	dbPath := store.DefaultIndexPath(dir)
	if !store.Exists(dbPath) {
		if human {
			output.WriteGummyStatus(os.Stdout, output.GummyStatusInfo{
				State: string(output.IndexMissing),
			})
			return nil
		}
		resp := output.Response[StatusResponse]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results: []StatusResponse{{
				State: output.IndexMissing,
			}},
			Meta: output.Meta{
				Command:    "status",
				RepoRoot:   dir,
				IndexState: output.IndexMissing,
				Ms:         w.Elapsed(),
				Total:      1,
			},
		}
		return w.WriteResponse(resp)
	}

	// Open store (read-only mode)
	s, err := store.Open(dbPath)
	if err != nil {
		return w.WriteError("status", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}
	defer s.Close()

	// Get stats
	symbols, refs, calls, err := s.GetStats()
	if err != nil {
		return w.WriteError("status", &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get stats: " + err.Error(),
		})
	}

	// Get meta values
	indexedAt, _ := s.GetMeta("indexed_at")
	commit, _ := s.GetMeta("git_commit")
	fingerprint, _ := s.GetMeta("fingerprint")

	// Check index state
	state := query.CheckIndexState(s.DB(), dir, Version)

	// Compute age description
	ageDesc := formatAge(indexedAt)

	// Get embedding and enrichment counts for enhanced status
	embedded, _ := s.CountEmbeddings()
	enriched := countEnrichedSymbols(s)

	if human {
		output.WriteGummyStatus(os.Stdout, output.GummyStatusInfo{
			State:       string(state),
			Commit:      commit,
			IndexedAt:   indexedAt,
			Symbols:     symbols,
			Refs:        refs,
			Calls:       calls,
			Fingerprint: fingerprint,
			AgeDesc:     ageDesc,
			Embedded:    embedded,
			Enriched:    enriched,
		})
		return nil
	}

	// JSON response
	resp := output.Response[StatusResponse]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results: []StatusResponse{{
			State:       state,
			Commit:      commit,
			IndexedAt:   indexedAt,
			Symbols:     symbols,
			Refs:        refs,
			Calls:       calls,
			Fingerprint: fingerprint,
		}},
		Meta: output.Meta{
			Command:    "status",
			RepoRoot:   dir,
			IndexState: state,
			Ms:         w.Elapsed(),
			Total:      1,
		},
	}

	return w.WriteResponse(resp)
}

// formatAge returns a human-readable age string from an RFC3339 timestamp.
func formatAge(timestamp string) string {
	if timestamp == "" {
		return ""
	}

	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ""
	}

	age := time.Since(t)

	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		mins := int(age.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return formatAgeUnit(mins, "m") + " ago"
	case age < 24*time.Hour:
		hours := int(age.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return formatAgeUnit(hours, "h") + " ago"
	default:
		days := int(age.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return formatAgeUnit(days, "d") + " ago"
	}
}

func formatAgeUnit(n int, unit string) string {
	return fmt.Sprintf("%d%s", n, unit)
}

// countEnrichedSymbols counts symbols with LLM-generated purposes.
func countEnrichedSymbols(s *store.Store) int {
	var count int
	_ = s.DB().QueryRow(`SELECT COUNT(*) FROM symbol_purposes`).Scan(&count)
	return count
}
