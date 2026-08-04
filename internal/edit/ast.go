// Package edit provides AST-aware editing operations for Go source code.
package edit

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/dkoosis/atomicfile"
)

// ErrSymbolNotFound is returned when a symbol cannot be located in the file.
var ErrSymbolNotFound = errors.New("symbol not found")

// ErrAppliedNotDurable signals that ApplyAndWrite landed the edit on disk
// (verified by read-back) but atomicfile's post-rename directory fsync failed,
// so durability of the directory entry is unconfirmed. Callers MUST treat this
// as applied — the returned Result's Applied field is true — and never retry,
// or they will double-apply insert_before/insert_after (Codex review, PR #234).
var ErrAppliedNotDurable = errors.New("applied but not confirmed durable")

// Operation defines the type of edit operation
type Operation string

const (
	OpReplaceBody  Operation = "replace_body"  // Keep signature, replace body
	OpReplaceFull  Operation = "replace_full"  // Replace entire symbol
	OpInsertAfter  Operation = "insert_after"  // Add code after symbol
	OpInsertBefore Operation = "insert_before" // Add code before symbol
)

// Request describes an edit operation
type Request struct {
	File      string    // File path to edit
	Symbol    string    // Symbol name to find
	Line      int       // Optional: specific line to target
	Operation Operation // Edit operation type
	NewCode   string    // New code to insert/replace
}

// Result contains the edit result
type Result struct {
	File         string `json:"file"`
	OriginalCode string `json:"original_code"`
	NewCode      string `json:"new_code"`
	Diff         string `json:"diff"`
	LineStart    int    `json:"line_start"`
	LineEnd      int    `json:"line_end"`
	NewLineEnd   int    `json:"new_line_end"`
	Applied      bool   `json:"applied"`

	formatted []byte // formatted file content, used by ApplyAndWrite
}

// FindSymbol locates a symbol in the file and returns its position info
func FindSymbol(filePath, symbolName string, optLine int) (*SymbolInfo, error) {
	src, err := os.ReadFile(filePath) // #nosec G304 -- filePath from symbol lookup
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}

	var found *SymbolInfo
	ast.Inspect(f, func(n ast.Node) bool {
		if found != nil {
			return false
		}

		switch decl := n.(type) {
		case *ast.FuncDecl:
			if decl.Name.Name == symbolName {
				if optLine > 0 && fset.Position(decl.Pos()).Line != optLine {
					return true
				}
				found = &SymbolInfo{
					Name:      symbolName,
					Kind:      "func",
					File:      filePath,
					Source:    src,
					Fset:      fset,
					Node:      decl,
					BodyStart: 0,
					BodyEnd:   0,
				}
				found.LineStart = fset.Position(decl.Pos()).Line
				found.LineEnd = fset.Position(decl.End()).Line
				found.ColStart = fset.Position(decl.Pos()).Column
				found.PosStart = int(decl.Pos())
				found.PosEnd = int(decl.End())

				if decl.Body != nil {
					found.BodyStart = int(decl.Body.Pos())
					found.BodyEnd = int(decl.Body.End())
					found.BodyLineStart = fset.Position(decl.Body.Pos()).Line
					found.BodyLineEnd = fset.Position(decl.Body.End()).Line
				}

				if decl.Recv != nil {
					found.Kind = "method"
				}
			}

		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.Name == symbolName {
						if optLine > 0 && fset.Position(decl.Pos()).Line != optLine {
							continue
						}
						found = &SymbolInfo{
							Name:      symbolName,
							Kind:      "type",
							File:      filePath,
							Source:    src,
							Fset:      fset,
							Node:      decl,
							LineStart: fset.Position(decl.Pos()).Line,
							LineEnd:   fset.Position(decl.End()).Line,
							ColStart:  fset.Position(decl.Pos()).Column,
							PosStart:  int(decl.Pos()),
							PosEnd:    int(decl.End()),
						}
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if name.Name == symbolName {
							if optLine > 0 && fset.Position(decl.Pos()).Line != optLine {
								continue
							}
							kind := "var"
							if decl.Tok == token.CONST {
								kind = "const"
							}
							found = &SymbolInfo{
								Name:      symbolName,
								Kind:      kind,
								File:      filePath,
								Source:    src,
								Fset:      fset,
								Node:      decl,
								LineStart: fset.Position(decl.Pos()).Line,
								LineEnd:   fset.Position(decl.End()).Line,
								ColStart:  fset.Position(decl.Pos()).Column,
								PosStart:  int(decl.Pos()),
								PosEnd:    int(decl.End()),
							}
						}
					}
				}
			}
		}
		return true
	})

	if found == nil {
		return nil, fmt.Errorf("%w: %q in %s", ErrSymbolNotFound, symbolName, filePath)
	}

	return found, nil
}

// SymbolInfo contains information about a found symbol
type SymbolInfo struct {
	Name          string
	Kind          string
	File          string
	Source        []byte
	Fset          *token.FileSet
	Node          ast.Node
	LineStart     int
	LineEnd       int
	ColStart      int
	PosStart      int
	PosEnd        int
	BodyStart     int // For functions: position of body '{'
	BodyEnd       int // For functions: position of body '}'
	BodyLineStart int
	BodyLineEnd   int
}

// OriginalCode returns the original source code for the symbol
func (s *SymbolInfo) OriginalCode() string {
	// Token positions are 1-indexed in the file
	start := s.PosStart - 1
	end := s.PosEnd - 1
	if start < 0 || end > len(s.Source) || start >= end {
		return ""
	}
	return string(s.Source[start:end])
}

// BodyCode returns just the body of a function (if applicable)
func (s *SymbolInfo) BodyCode() string {
	if s.BodyStart == 0 || s.BodyEnd == 0 {
		return ""
	}
	start := s.BodyStart - 1
	end := s.BodyEnd - 1
	if start < 0 || end > len(s.Source) || start >= end {
		return ""
	}
	return string(s.Source[start:end])
}

// Apply performs the edit operation and returns the result
func Apply(req Request) (*Result, error) {
	info, err := FindSymbol(req.File, req.Symbol, req.Line)
	if err != nil {
		return nil, err
	}

	var newContent []byte
	var originalCode, newCode string

	switch req.Operation {
	case OpReplaceBody:
		if info.Kind != "func" && info.Kind != "method" {
			return nil, fmt.Errorf("replace_body only works on functions/methods, got %s", info.Kind)
		}
		if info.BodyStart == 0 {
			return nil, fmt.Errorf("function %s has no body (interface method?)", req.Symbol)
		}
		originalCode = info.BodyCode()
		newCode = req.NewCode

		// Ensure new code has braces
		newCode = strings.TrimSpace(newCode)
		if !strings.HasPrefix(newCode, "{") {
			newCode = "{\n" + newCode + "\n}"
		}

		// Replace the body
		start := info.BodyStart - 1
		end := info.BodyEnd - 1
		newContent = make([]byte, 0, len(info.Source)-len(originalCode)+len(newCode))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpReplaceFull:
		originalCode = info.OriginalCode()
		newCode = strings.TrimSpace(req.NewCode)

		start := info.PosStart - 1
		end := info.PosEnd - 1
		newContent = make([]byte, 0, len(info.Source)-len(originalCode)+len(newCode))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpInsertAfter:
		originalCode = info.OriginalCode()
		newCode = "\n\n" + strings.TrimSpace(req.NewCode)

		end := info.PosEnd - 1
		newContent = make([]byte, 0, len(info.Source)+len(newCode))
		newContent = append(newContent, info.Source[:end]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpInsertBefore:
		originalCode = info.OriginalCode()
		newCode = strings.TrimSpace(req.NewCode) + "\n\n"

		start := info.PosStart - 1
		newContent = make([]byte, 0, len(info.Source)+len(newCode))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[start:]...)

	default:
		return nil, fmt.Errorf("unknown operation: %s", req.Operation)
	}

	// Format the result — a format failure means the edit produced invalid Go,
	// so surface it rather than silently writing broken code.
	formatted, err := format.Source(newContent)
	if err != nil {
		return nil, fmt.Errorf("format after edit: %w", err)
	}

	// Compute diff
	diff := computeDiff(string(info.Source), string(formatted))

	// Count new line end
	newLineEnd := info.LineStart + strings.Count(newCode, "\n")

	return &Result{
		File:         req.File,
		OriginalCode: originalCode,
		NewCode:      newCode,
		Diff:         diff,
		LineStart:    info.LineStart,
		LineEnd:      info.LineEnd,
		NewLineEnd:   newLineEnd,
		Applied:      false,
		formatted:    formatted,
	}, nil
}

// ApplyAndWrite performs the edit and writes to disk
func ApplyAndWrite(req Request) (*Result, error) {
	result, err := Apply(req)
	if err != nil {
		return nil, err
	}

	// Preserve the source file's original permissions. atomicfile.WriteFile applies
	// perm only when the target is new and keeps an existing file's mode, so an
	// existing checked-in file retains its 0644 (or whatever) regardless. perm here
	// sets the mode for the new-file case; default 0644.
	perm := os.FileMode(0644)
	if info, statErr := os.Stat(req.File); statErr == nil {
		perm = info.Mode().Perm()
	}

	// Atomic write: a crash mid-write leaves the original file untouched.
	// os.WriteFile truncates first, which can corrupt user source on disk-full / SIGKILL.
	if err := atomicfile.WriteFile(req.File, result.formatted, perm); err != nil {
		// atomicfile.WriteFile can fail after the rename already landed — the
		// parent-directory fsync is a separate final step, and it doesn't
		// expose a sentinel distinguishing "never renamed" from "renamed,
		// durability of the directory entry unconfirmed". Read the file back:
		// if it matches, the edit IS on disk, so report Applied=true rather
		// than let a caller retry and double-apply insert_before/insert_after
		// (CR review, PR #234).
		if onDisk, readErr := os.ReadFile(req.File); readErr == nil && bytes.Equal(onDisk, result.formatted) {
			result.Applied = true
			return result, fmt.Errorf("%w: %w", ErrAppliedNotDurable, err)
		}
		return nil, fmt.Errorf("write file: %w", err)
	}

	result.Applied = true
	return result, nil
}

// computeDiff generates a unified diff using proper LCS algorithm.
func computeDiff(original, modified string) string {
	if original == modified {
		return ""
	}

	diff := difflib.UnifiedDiff{
		A:        strings.Split(original, "\n"),
		B:        strings.Split(modified, "\n"),
		FromFile: "original",
		ToFile:   "modified",
		Context:  3,
	}

	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Sprintf("--- original\n+++ modified\n@@ error generating diff: %v @@\n", err)
	}

	return result
}
