package query

import "testing"

func TestParsePosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    PositionQuery
		wantErr bool
	}{
		{
			name:  "file_line_col",
			input: "main.go:12:4",
			want: PositionQuery{
				File: "main.go",
				Line: 12,
				Col:  4,
			},
		},
		{
			name:  "file_line_default_col",
			input: "internal/query/position.go:27",
			want: PositionQuery{
				File: "internal/query/position.go",
				Line: 27,
				Col:  1,
			},
		},
		{
			name:  "windows_file_line_col",
			input: `C:\repo\main.go:22:3`,
			want: PositionQuery{
				File: `C:\repo\main.go`,
				Line: 22,
				Col:  3,
			},
		},
		{
			name:  "windows_file_line",
			input: `C:\repo\main.go:7`,
			want: PositionQuery{
				File: `C:\repo\main.go`,
				Line: 7,
				Col:  1,
			},
		},
		{
			name:    "invalid",
			input:   "nope",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePosition(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.File != tt.want.File || got.Line != tt.want.Line || got.Col != tt.want.Col {
				t.Fatalf("unexpected result: %+v", *got)
			}
		})
	}
}
