package index

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// WorkspacePatterns returns load patterns for go/packages.
// If go.work exists at dir, returns a pattern per workspace module.
// Otherwise returns ["./..."].
func WorkspacePatterns(dir string) ([]string, error) {
	goWorkPath := filepath.Join(dir, "go.work")

	data, err := os.ReadFile(goWorkPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{patternAllPkgs}, nil
		}
		return nil, fmt.Errorf("read go.work: %w", err)
	}

	wf, err := modfile.ParseWork(goWorkPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.work: %w", err)
	}

	if len(wf.Use) == 0 {
		return []string{patternAllPkgs}, nil
	}

	patterns := make([]string, 0, len(wf.Use))
	for _, u := range wf.Use {
		patterns = append(patterns, u.Path+"/...")
	}
	return patterns, nil
}
