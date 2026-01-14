package query

import (
	"testing"
)

func TestParsePosition(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *PositionQuery
		wantErr bool
	}{
		{
			name:  "full position",
			input: "main.go:42:12",
			want:  &PositionQuery{File: "main.go", Line: 42, Col: 12},
		},
		{
			name:  "line only",
			input: "main.go:42",
			want:  &PositionQuery{File: "main.go", Line: 42, Col: 1},
		},
		{
			name:  "path with directory",
			input: "internal/index/symbols.go:100:5",
			want:  &PositionQuery{File: "internal/index/symbols.go", Line: 100, Col: 5},
		},
		{
			name:  "absolute path",
			input: "/Users/test/project/main.go:10:1",
			want:  &PositionQuery{File: "/Users/test/project/main.go", Line: 10, Col: 1},
		},
		{
			name:    "missing line",
			input:   "main.go",
			wantErr: true,
		},
		{
			name:    "invalid line number",
			input:   "main.go:abc",
			wantErr: true,
		},
		{
			name:    "invalid column number",
			input:   "main.go:42:abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePosition(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePosition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.File != tt.want.File {
				t.Errorf("File = %q, want %q", got.File, tt.want.File)
			}
			if got.Line != tt.want.Line {
				t.Errorf("Line = %d, want %d", got.Line, tt.want.Line)
			}
			if got.Col != tt.want.Col {
				t.Errorf("Col = %d, want %d", got.Col, tt.want.Col)
			}
		})
	}
}
