package cmd

import (
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/query"
)

// snipe-8u1: bare-name resolution must prefer in-repo func/method over
// external/stale candidates, and must error rather than pick a foreign hit
// when nothing in-repo qualifies.
func TestPickFlowEntry(t *testing.T) {
	staleMain := query.SymbolRow{
		ID: "stale1", Name: "main", Kind: "package",
		FilePath: "/Users/x/Library/Caches/go-build/foo.go", FilePathRel: "",
	}
	repoMain := query.SymbolRow{
		ID: "repo1", Name: "main", Kind: "func",
		FilePath: "/repo/cmd/main.go", FilePathRel: "cmd/main.go",
	}
	repoVar := query.SymbolRow{
		ID: "repo2", Name: "main", Kind: "var",
		FilePath: "/repo/cmd/main.go", FilePathRel: "cmd/main.go",
	}

	tests := []struct {
		name    string
		syms    []query.SymbolRow
		wantID  string
		wantErr string
	}{
		{
			name:   "prefers in-repo func over stale package hit",
			syms:   []query.SymbolRow{staleMain, repoMain},
			wantID: "repo1",
		},
		{
			name:   "stale-first ordering still picks in-repo",
			syms:   []query.SymbolRow{staleMain, repoVar, repoMain},
			wantID: "repo1",
		},
		{
			name:   "falls back to in-repo non-func when no func exists",
			syms:   []query.SymbolRow{staleMain, repoVar},
			wantID: "repo2",
		},
		{
			name:    "no in-repo candidate returns clear error",
			syms:    []query.SymbolRow{staleMain},
			wantErr: "outside this repo",
		},
		{
			name:    "empty list returns not-found error",
			syms:    nil,
			wantErr: "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pickFlowEntry("main", tt.syms)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.ID != tt.wantID {
				t.Errorf("got ID %q, want %q", got.ID, tt.wantID)
			}
		})
	}
}
