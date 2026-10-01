package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashAndCountLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{name: "empty", content: "", want: 0},
		{name: "trailing newline", content: "package a\n\nfunc F() {}\n", want: 3},
		{name: "no trailing newline", content: "package a\nfunc F() {}", want: 2},
		{name: "crlf", content: "package a\r\nfunc F() {}\r\n", want: 2},
		{name: "blank lines count", content: "\n\n\n", want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.go")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			hash, lines, err := hashAndCountLines(path)
			if err != nil {
				t.Fatalf("hashAndCountLines: %v", err)
			}
			if lines != tt.want {
				t.Errorf("lines = %d, want %d", lines, tt.want)
			}
			// The hash must stay the one change detection already stored.
			want, err := HashFileSHA256(path)
			if err != nil {
				t.Fatal(err)
			}
			if hash != want {
				t.Errorf("hash = %s, want HashFileSHA256's %s", hash, want)
			}
		})
	}
}

func TestComputeFileInfo_CarriesLineCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte("package a\n\nfunc F() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := computeFileInfo(path)
	if err != nil {
		t.Fatalf("computeFileInfo: %v", err)
	}
	if info.Lines != 3 {
		t.Errorf("Lines = %d, want 3", info.Lines)
	}
}
