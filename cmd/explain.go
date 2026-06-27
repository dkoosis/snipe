package cmd

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	explainMode     string
	explainWarnings string
	explainAt       string
)

func runExplain(args []string) error {
	start := time.Now()

	w := output.NewWriter(os.Stdout, GetOutputFormat())

	// Need either a symbol name or --at position
	if len(args) == 0 && explainAt == "" {
		return w.WriteError(cmdNameExplain, &output.Error{
			Code:    output.ErrInternal,
			Message: errProvideSymbolOrAt,
		})
	}

	// Open store
	s, dir, err := OpenStore(w, cmdNameExplain)
	if err != nil {
		return err
	}
	defer s.Close()

	var symbolID string
	var queryInfo map[string]string

	if explainAt != "" {
		// Resolve position
		pos, err := query.ParsePosition(explainAt)
		if err != nil {
			return w.WriteError(cmdNameExplain, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		// Make path absolute if relative
		if !filepath.IsAbs(pos.File) {
			pos.File = filepath.Join(dir, pos.File)
		}

		symbolID, err = query.ResolvePosition(s.DB(), pos)
		if err != nil {
			return w.WriteError(cmdNameExplain, &output.Error{
				Code:    output.ErrNotFound,
				Message: err.Error(),
			})
		}
		queryInfo = map[string]string{"at": explainAt}
	} else {
		name := args[0]

		// Check if input looks like a symbol ID (16-char hex string)
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				goto explain
			}
		}

		// Check for file:Symbol syntax
		if idx := strings.LastIndex(name, ":"); idx > 0 && !strings.Contains(name[idx:], "/") {
			filePart := name[:idx]
			symbolPart := name[idx+1:]
			if symbolPart != "" && !strings.Contains(symbolPart, ":") {
				symbols, err := query.LookupByNameInFile(s.DB(), symbolPart, filePart)
				if err != nil {
					return w.WriteError(cmdNameExplain, &output.Error{
						Code:    output.ErrInternal,
						Message: err.Error(),
					})
				}
				if len(symbols) == 1 {
					symbolID = symbols[0].ID
					queryInfo = map[string]string{flagSymbol: symbolPart, flagFile: filePart}
					goto explain
				} else if len(symbols) > 1 {
					candidates := make([]output.Candidate, len(symbols))
					for i := range symbols {
						sym := &symbols[i]
						candidates[i] = sym.ToCandidate()
					}
					return w.WriteError(cmdNameExplain, output.NewAmbiguousError(name, candidates))
				}
			}
		}

		// Look up by name
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameExplain, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			maxDist := query.DefaultMaxDistance(name)
			suggestions, err := query.FindSimilarSymbols(s.DB(), name, maxDist, 3)
			if err != nil {
				return w.WriteError(cmdNameExplain, output.NewNotFoundError(name))
			}
			return w.WriteError(cmdNameExplain, output.NewNotFoundError(name, suggestions...))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i := range symbols {
				sym := &symbols[i]
				candidates[i] = sym.ToCandidate()
			}
			return w.WriteError(cmdNameExplain, output.NewAmbiguousError(name, candidates))
		}

		symbolID = symbols[0].ID
		queryInfo = map[string]string{flagSymbol: name}
	}

explain:
	// Parse options
	opts := query.DefaultExplainOptions()

	switch explainMode {
	case "brief":
		opts.Mode = output.ExplainBrief
	case "normal":
		opts.Mode = output.ExplainNormal
	case "deep":
		opts.Mode = output.ExplainDeep
	default:
		return w.WriteError(cmdNameExplain, &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid --mode: use brief, normal, or deep",
		})
	}

	switch explainWarnings {
	case "none":
		opts.WarningsMode = output.WarningsNone
	case "fast":
		opts.WarningsMode = output.WarningsFast
	case "full":
		opts.WarningsMode = output.WarningsFull
	default:
		return w.WriteError(cmdNameExplain, &output.Error{
			Code:    output.ErrInternal,
			Message: "invalid --warnings: use none, fast, or full",
		})
	}

	// Look up symbol for staleness check (Explain does this internally too)
	var staleFiles []string
	if sym, lookupErr := query.LookupByID(s.DB(), symbolID); lookupErr == nil && sym != nil {
		staleFiles = query.CheckPathStaleness(s.DB(), dir, []string{sym.FilePath})
	}

	// Run explain
	result, err := query.Explain(s.DB(), symbolID, opts)
	if err != nil {
		return w.WriteError(cmdNameExplain, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	resp := output.Response[output.ExplainResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.ExplainResult{*result},
		Meta: output.Meta{
			Command:    cmdNameExplain,
			Query:      queryInfo,
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      1,
			StaleFiles: staleFiles,
		},
	}

	return w.WriteResponse(resp)
}
