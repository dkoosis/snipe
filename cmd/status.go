package cmd

import (
	"os"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

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

func runStatus() error {
	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	// Find repo root
	cwd, err := os.Getwd()
	if err != nil {
		return w.WriteError(cmdNameStatus, &output.Error{
			Code:    output.ErrInternal,
			Message: "failed to get working directory: " + err.Error(),
		})
	}
	dir := util.FindProjectRoot(cwd)
	if dir == "" {
		return w.WriteError(cmdNameStatus, &output.Error{
			Code:    output.ErrInternal,
			Message: "not in a git repository",
		})
	}

	// Check if index exists
	dbPath := store.DefaultIndexPath(dir)
	if !store.Exists(dbPath) {
		resp := output.Response[StatusResponse]{
			Protocol: output.ProtocolVersion,
			Ok:       true,
			Results: []StatusResponse{{
				State: output.IndexMissing,
			}},
			Meta: output.Meta{
				Command:    cmdNameStatus,
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
		return w.WriteError(cmdNameStatus, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}
	defer s.Close()

	// Get stats
	symbols, refs, calls, err := s.GetStats()
	if err != nil {
		return w.WriteError(cmdNameStatus, &output.Error{
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
			Command:    cmdNameStatus,
			RepoRoot:   dir,
			IndexState: state,
			Ms:         w.Elapsed(),
			Total:      1,
		},
	}

	return w.WriteResponse(resp)
}
