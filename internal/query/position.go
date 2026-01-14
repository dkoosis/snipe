package query

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// PositionQuery represents a file:line:col query
type PositionQuery struct {
	File string
	Line int
	Col  int
}

// ParsePosition parses a position string like "file.go:42:12"
func ParsePosition(s string) (*PositionQuery, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid position format: %s (expected file:line or file:line:col)", s)
	}

	line, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		// Might be file:line format
		if len(parts) == 2 {
			line, err = strconv.Atoi(parts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid line number: %s", parts[1])
			}
			return &PositionQuery{
				File: parts[0],
				Line: line,
				Col:  1,
			}, nil
		}
		return nil, fmt.Errorf("invalid line number: %s", parts[len(parts)-2])
	}

	col := 1
	if len(parts) >= 3 {
		col, err = strconv.Atoi(parts[len(parts)-1])
		if err != nil {
			return nil, fmt.Errorf("invalid column number: %s", parts[len(parts)-1])
		}
		// Reconstruct file path (might contain colons on Windows)
		return &PositionQuery{
			File: strings.Join(parts[:len(parts)-2], ":"),
			Line: line,
			Col:  col,
		}, nil
	}

	return &PositionQuery{
		File: strings.Join(parts[:len(parts)-1], ":"),
		Line: line,
		Col:  col,
	}, nil
}

// ResolvePosition finds the symbol at or near the given position
func ResolvePosition(db *sql.DB, pos *PositionQuery) (symbolID string, err error) {
	// First, try to find a reference at exactly this position
	err = db.QueryRow(`
		SELECT symbol_id FROM refs
		WHERE file_path LIKE ? AND line = ? AND col = ?
		LIMIT 1
	`, "%"+pos.File, pos.Line, pos.Col).Scan(&symbolID)

	if err == nil {
		return symbolID, nil
	}

	if err != sql.ErrNoRows {
		return "", fmt.Errorf("query refs: %w", err)
	}

	// Try to find a reference on this line, closest to the column
	err = db.QueryRow(`
		SELECT symbol_id FROM refs
		WHERE file_path LIKE ? AND line = ?
		ORDER BY ABS(col - ?)
		LIMIT 1
	`, "%"+pos.File, pos.Line, pos.Col).Scan(&symbolID)

	if err == nil {
		return symbolID, nil
	}

	if err != sql.ErrNoRows {
		return "", fmt.Errorf("query refs by line: %w", err)
	}

	// Try to find a symbol definition at this position
	err = db.QueryRow(`
		SELECT id FROM symbols
		WHERE file_path LIKE ? AND line_start = ? AND col_start <= ? AND col_end >= ?
		LIMIT 1
	`, "%"+pos.File, pos.Line, pos.Col, pos.Col).Scan(&symbolID)

	if err == nil {
		return symbolID, nil
	}

	if err != sql.ErrNoRows {
		return "", fmt.Errorf("query symbols: %w", err)
	}

	// Try to find a symbol that spans this line
	err = db.QueryRow(`
		SELECT id FROM symbols
		WHERE file_path LIKE ? AND line_start <= ? AND line_end >= ?
		ORDER BY (line_end - line_start)
		LIMIT 1
	`, "%"+pos.File, pos.Line, pos.Line).Scan(&symbolID)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no symbol found at %s:%d:%d", pos.File, pos.Line, pos.Col)
	}

	return symbolID, err
}
