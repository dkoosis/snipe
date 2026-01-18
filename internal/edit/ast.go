// Package edit provides AST-aware editing operations for Go source code.
package edit

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// Operation defines the type of edit operation
type Operation string

const (
	OpReplaceBody   Operation = "replace_body"   // Keep signature, replace body
	OpReplaceFull   Operation = "replace_full"   // Replace entire symbol
	OpInsertAfter   Operation = "insert_after"   // Add code after symbol
	OpInsertBefore  Operation = "insert_before"  // Add code before symbol
)

// ValidOperations returns all valid operation types
func ValidOperations() []Operation {
	return []Operation{OpReplaceBody, OpReplaceFull, OpInsertAfter, OpInsertBefore}
}

// Request describes an edit operation
type Request struct {
	File       string    // File path to edit
	Symbol     string    // Symbol name to find
	Line       int       // Optional: specific line to target
	Operation  Operation // Edit operation type
	NewCode    string    // New code to insert/replace
	ExpectedHash string  // Optional: hash to validate freshness
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
}

// FindSymbol locates a symbol in the file and returns its position info
func FindSymbol(filePath, symbolName string, optLine int) (*SymbolInfo, error) {
	src, err := os.ReadFile(filePath)
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
		return nil, fmt.Errorf("symbol %q not found in %s", symbolName, filePath)
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

	// Format the result
	formatted, err := format.Source(newContent)
	if err != nil {
		// Return unformatted if formatting fails
		formatted = newContent
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
	}, nil
}

// ApplyAndWrite performs the edit and writes to disk
func ApplyAndWrite(req Request) (*Result, error) {
	result, err := Apply(req)
	if err != nil {
		return nil, err
	}

	info, err := FindSymbol(req.File, req.Symbol, req.Line)
	if err != nil {
		return nil, err
	}

	// Rebuild the full content
	var newContent []byte
	switch req.Operation {
	case OpReplaceBody:
		newCode := strings.TrimSpace(req.NewCode)
		if !strings.HasPrefix(newCode, "{") {
			newCode = "{\n" + newCode + "\n}"
		}
		start := info.BodyStart - 1
		end := info.BodyEnd - 1
		newContent = make([]byte, 0, len(info.Source))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpReplaceFull:
		newCode := strings.TrimSpace(req.NewCode)
		start := info.PosStart - 1
		end := info.PosEnd - 1
		newContent = make([]byte, 0, len(info.Source))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpInsertAfter:
		newCode := "\n\n" + strings.TrimSpace(req.NewCode)
		end := info.PosEnd - 1
		newContent = make([]byte, 0, len(info.Source)+len(newCode))
		newContent = append(newContent, info.Source[:end]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[end:]...)

	case OpInsertBefore:
		newCode := strings.TrimSpace(req.NewCode) + "\n\n"
		start := info.PosStart - 1
		newContent = make([]byte, 0, len(info.Source)+len(newCode))
		newContent = append(newContent, info.Source[:start]...)
		newContent = append(newContent, []byte(newCode)...)
		newContent = append(newContent, info.Source[start:]...)
	}

	// Format
	formatted, err := format.Source(newContent)
	if err != nil {
		formatted = newContent
	}

	// Write to file
	if err := os.WriteFile(req.File, formatted, 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	result.Applied = true
	return result, nil
}

// computeDiff generates a simple unified diff
func computeDiff(original, modified string) string {
	oldLines := strings.Split(original, "\n")
	newLines := strings.Split(modified, "\n")

	var diff bytes.Buffer
	diff.WriteString("--- original\n")
	diff.WriteString("+++ modified\n")

	// Simple line-by-line comparison (not a full diff algorithm)
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	inHunk := false
	hunkStart := 0
	var hunkBuf bytes.Buffer

	for i := 0; i < maxLines; i++ {
		var oldLine, newLine string
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if !inHunk {
				inHunk = true
				hunkStart = i + 1
				// Add context before
				if i > 0 {
					hunkBuf.WriteString(" " + oldLines[i-1] + "\n")
				}
			}
			if i < len(oldLines) && oldLine != "" {
				hunkBuf.WriteString("-" + oldLine + "\n")
			}
			if i < len(newLines) && newLine != "" {
				hunkBuf.WriteString("+" + newLine + "\n")
			}
		} else if inHunk {
			// Context line after change
			hunkBuf.WriteString(" " + oldLine + "\n")
			// Flush hunk
			diff.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", hunkStart, hunkStart))
			diff.Write(hunkBuf.Bytes())
			hunkBuf.Reset()
			inHunk = false
		}
	}

	// Flush remaining hunk
	if inHunk {
		diff.WriteString(fmt.Sprintf("@@ -%d +%d @@\n", hunkStart, hunkStart))
		diff.Write(hunkBuf.Bytes())
	}

	return diff.String()
}
