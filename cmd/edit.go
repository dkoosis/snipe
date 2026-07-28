package cmd

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/edit"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// editErrCode maps an edit.Apply/ApplyAndWrite error to an output error code.
// edit.ErrSymbolNotFound is a public sentinel, so surface it as NOT_FOUND:
// Claude (D1) must see a recoverable lookup miss, not an INTERNAL_ERROR crash.
func editErrCode(err error) string {
	if errors.Is(err, edit.ErrSymbolNotFound) {
		return output.ErrNotFound
	}
	return output.ErrInternal
}

var (
	editOperation   string
	editNewCode     string
	editNewCodeFile string
	editApply       bool
	editBatch       bool
	editAt          string
)

// EditResponse contains the result of an edit operation
type EditResponse struct {
	File         string `json:"file"`
	Symbol       string `json:"symbol"`
	Operation    string `json:"operation"`
	OriginalCode string `json:"original_code,omitempty"`
	NewCode      string `json:"new_code,omitempty"`
	Diff         string `json:"diff,omitempty"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`
	NewLineEnd   int    `json:"new_line_end,omitempty"`
	Applied      bool   `json:"applied"`
}

// BatchEditRequest for batch mode
type BatchEditRequest struct {
	Symbol    string `json:"symbol"`
	Operation string `json:"operation"`
	NewCode   string `json:"new_code"`
	File      string `json:"file,omitempty"`
}

func runEdit(args []string) error {
	start := time.Now()

	w := output.NewWriter(os.Stdout, GetOutputFormat())

	// Handle batch mode
	if editBatch {
		return runBatchEdit(w, start)
	}

	// Single edit mode - need symbol name or --at position
	if len(args) == 0 && editAt == "" {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    output.ErrInternal,
			Message: errProvideSymbolOrAt,
		})
	}

	if editOperation == "" {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    output.ErrInternal,
			Message: "provide --operation: replace_body, replace_full, insert_after, insert_before",
		})
	}

	// Get new code
	newCode := editNewCode
	if editNewCodeFile != "" {
		data, err := os.ReadFile(editNewCodeFile) // #nosec G304 -- CLI tool accepts user-specified file paths
		if err != nil {
			return w.WriteError(cmdNameEdit, &output.Error{
				Code:    output.ErrInternal,
				Message: "read new-code-file: " + err.Error(),
			})
		}
		newCode = string(data)
	}

	if newCode == "" {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    output.ErrInternal,
			Message: "provide --new-code or --new-code-file",
		})
	}

	// Find repo root and open store
	s, dir, err := OpenStore(w, cmdNameEdit)
	if err != nil {
		return err
	}
	defer s.Close()

	// Resolve symbol
	var filePath string
	var symbolName string

	// Handle --at position resolution
	if editAt != "" {
		pos, err := query.ParsePosition(editAt)
		if err != nil {
			return w.WriteError(cmdNameEdit, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		// Make path absolute if relative
		if !filepath.IsAbs(pos.File) {
			pos.File = filepath.Join(dir, pos.File)
		}

		symbolID, err := query.ResolvePosition(s.DB(), pos)
		if err != nil {
			return w.WriteError(cmdNameEdit, &output.Error{
				Code:    output.ErrNotFound,
				Message: err.Error(),
			})
		}

		sym, err := query.LookupByID(s.DB(), symbolID)
		if err != nil || sym == nil {
			return w.WriteError(cmdNameEdit, &output.Error{
				Code:    output.ErrNotFound,
				Message: "symbol not found at position",
			})
		}

		filePath = sym.FilePath
		symbolName = sym.Name
		goto doEdit
	}

	// Resolve by name - block scopes name to avoid goto issues
	{
		name := args[0]

		// Check if input is a hex ID
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				sym, err := query.LookupByID(s.DB(), name)
				if err != nil || sym == nil {
					return w.WriteError(cmdNameEdit, &output.Error{
						Code:    output.ErrNotFound,
						Message: "symbol not found: " + name,
					})
				}
				filePath = sym.FilePath
				symbolName = sym.Name
				goto doEdit
			}
		}

		// Check for file:Symbol syntax
		if idx := strings.LastIndex(name, ":"); idx > 0 {
			possibleFile := name[:idx]
			possibleSymbol := name[idx+1:]
			if !strings.Contains(possibleSymbol, ":") {
				symbols, err := query.LookupByNameInFile(s.DB(), possibleSymbol, possibleFile)
				if err == nil && len(symbols) == 1 {
					filePath = symbols[0].FilePath
					symbolName = symbols[0].Name
					goto doEdit
				}
			}
		}

		// Regular name lookup
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameEdit, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			return w.WriteError(cmdNameEdit, output.NewNotFoundError(name))
		}

		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i := range symbols {
				candidates[i] = symbols[i].ToCandidate()
			}
			return w.WriteError(cmdNameEdit, output.NewAmbiguousError(name, candidates))
		}

		filePath = symbols[0].FilePath
		symbolName = symbols[0].Name
	}

doEdit:
	// Make path absolute
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(dir, filePath)
	}

	// Create edit request
	req := edit.Request{
		File:      filePath,
		Symbol:    symbolName,
		Operation: edit.Operation(editOperation),
		NewCode:   newCode,
	}

	var result *edit.Result
	if editApply {
		result, err = edit.ApplyAndWrite(req)
	} else {
		result, err = edit.Apply(req)
	}

	if err != nil {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    editErrCode(err),
			Message: err.Error(),
		})
	}

	// Build response
	relPath, _ := filepath.Rel(dir, filePath)
	if relPath == "" {
		relPath = filePath
	}

	editResp := EditResponse{
		File:         relPath,
		Symbol:       symbolName,
		Operation:    editOperation,
		OriginalCode: result.OriginalCode,
		NewCode:      result.NewCode,
		Diff:         result.Diff,
		LineStart:    result.LineStart,
		LineEnd:      result.LineEnd,
		NewLineEnd:   result.NewLineEnd,
		Applied:      result.Applied,
	}

	resp := output.Response[EditResponse]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []EditResponse{editResp},
		Meta: output.Meta{
			Command:  cmdNameEdit,
			Query:    map[string]string{flagSymbol: symbolName, "operation": editOperation},
			RepoRoot: dir,
			Ms:       time.Since(start).Milliseconds(),
			Total:    1,
		},
	}

	return emitEdit(w, resp)
}

func runBatchEdit(w *output.Writer, start time.Time) error {
	// Read batch operations from stdin
	var requests []BatchEditRequest
	dec := json.NewDecoder(os.Stdin)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&requests); err != nil {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    output.ErrInternal,
			Message: "parse batch input: " + err.Error(),
		})
	}

	if len(requests) == 0 {
		return w.WriteError(cmdNameEdit, &output.Error{
			Code:    output.ErrInternal,
			Message: "no operations in batch",
		})
	}

	s, dir, err := OpenStore(w, cmdNameEdit)
	if err != nil {
		return err
	}
	defer s.Close()

	results := make([]EditResponse, 0, len(requests))
	var degraded []string

	for _, req := range requests {
		// Resolve symbol
		symbols, err := query.LookupByName(s.DB(), req.Symbol)
		if err != nil || len(symbols) == 0 {
			degraded = append(degraded, fmt.Sprintf("symbol_not_found:%s", req.Symbol))
			continue
		}
		if len(symbols) > 1 && req.File == "" {
			degraded = append(degraded, fmt.Sprintf("ambiguous:%s", req.Symbol))
			continue
		}

		sym := &symbols[0]
		if req.File != "" {
			// Find matching file
			found := false
			for i := range symbols {
				s := &symbols[i]
				if strings.Contains(s.FilePath, req.File) || strings.Contains(s.FilePathRel, req.File) {
					sym = s
					found = true
					break
				}
			}
			if !found {
				degraded = append(degraded, fmt.Sprintf("file_not_matched:%s:%s", req.Symbol, req.File))
				continue
			}
		}

		filePath := sym.FilePath
		if !filepath.IsAbs(filePath) {
			filePath = filepath.Join(dir, filePath)
		}

		editReq := edit.Request{
			File:      filePath,
			Symbol:    sym.Name,
			Operation: edit.Operation(req.Operation),
			NewCode:   req.NewCode,
		}

		var result *edit.Result
		if editApply {
			result, err = edit.ApplyAndWrite(editReq)
		} else {
			result, err = edit.Apply(editReq)
		}

		if err != nil {
			degraded = append(degraded, fmt.Sprintf("edit_failed:%s:%s", req.Symbol, err.Error()))
			continue
		}

		relPath, _ := filepath.Rel(dir, filePath)
		if relPath == "" {
			relPath = filePath
		}

		results = append(results, EditResponse{
			File:         relPath,
			Symbol:       sym.Name,
			Operation:    req.Operation,
			OriginalCode: result.OriginalCode,
			NewCode:      result.NewCode,
			Diff:         result.Diff,
			LineStart:    result.LineStart,
			LineEnd:      result.LineEnd,
			NewLineEnd:   result.NewLineEnd,
			Applied:      result.Applied,
		})
	}

	resp := output.Response[EditResponse]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:  cmdNameEdit,
			Query:    map[string]string{"mode": "batch"},
			RepoRoot: dir,
			Degraded: uniqueStrings(degraded),
			Ms:       time.Since(start).Milliseconds(),
			Total:    len(results),
		},
	}

	return emitEdit(w, resp)
}

// emitEdit renders edit results. Default (Claude) surface is a terse status
// line per edit plus the unified diff; --format json emits the full envelope
// (D1). Dry-run vs applied is stated explicitly.
func emitEdit(w *output.Writer, resp output.Response[EditResponse]) error {
	if GetOutputFormat() == output.OutputJSON {
		return w.WriteResponse(resp)
	}
	var b strings.Builder
	for i := range resp.Results {
		r := &resp.Results[i]
		mode := "dry-run"
		if r.Applied {
			mode = "applied"
		}
		fmt.Fprintf(&b, "edit · %s · %s:%d-%d · %s\n", r.Operation, r.File, r.LineStart, r.LineEnd, mode)
		if r.Diff != "" {
			b.WriteString(r.Diff)
			if !strings.HasSuffix(r.Diff, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	// Degraded ops (symbol not found, ambiguous, edit failed) never land in
	// Results — surface them so a batch where every op fails isn't silent.
	if len(resp.Meta.Degraded) > 0 {
		fmt.Fprintf(&b, "edit · %d degraded\n", len(resp.Meta.Degraded))
		for _, d := range resp.Meta.Degraded {
			fmt.Fprintf(&b, "  ✗ %s\n", d)
		}
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
