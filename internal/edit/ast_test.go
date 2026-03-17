package edit_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/edit"
)

const sampleSource = `package sample

type Thing struct{}

const Meaning = 42

func Add(a, b int) int {
	return a + b
}

func (Thing) Name() string {
	return "thing"
}
`

func writeGoFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.go")
	require.NoError(t, os.WriteFile(path, []byte(sampleSource), 0o600))
	return path
}

func TestFindSymbol_ReturnsSymbolMetadata_When_MatchingDeclarationExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		symbol      string
		wantKind    string
		wantLine    int
		wantHasBody bool
	}{
		{name: "function declaration", symbol: "Add", wantKind: "func", wantLine: 7, wantHasBody: true},
		{name: "method declaration", symbol: "Name", wantKind: "method", wantLine: 11, wantHasBody: true},
		{name: "type declaration", symbol: "Thing", wantKind: "type", wantLine: 3, wantHasBody: false},
		{name: "const declaration", symbol: "Meaning", wantKind: "const", wantLine: 5, wantHasBody: false},
	}

	path := writeGoFile(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			info, err := edit.FindSymbol(path, tc.symbol, 0)
			require.NoError(t, err)

			assert.Equal(t, tc.symbol, info.Name)
			assert.Equal(t, tc.wantKind, info.Kind)
			assert.Equal(t, tc.wantLine, info.LineStart)
			assert.GreaterOrEqual(t, info.LineEnd, info.LineStart)
			assert.NotEmpty(t, info.OriginalCode())
			if tc.wantHasBody {
				assert.NotEmpty(t, info.BodyCode())
			} else {
				assert.Empty(t, info.BodyCode())
			}
		})
	}
}

func TestFindSymbol_ReturnsError_When_SymbolDoesNotExistOrLineFilterMismatches(t *testing.T) {
	t.Parallel()

	path := writeGoFile(t)
	tests := []struct {
		name    string
		symbol  string
		line    int
		wantErr string
	}{
		{name: "missing symbol", symbol: "Unknown", line: 0, wantErr: `"Unknown"`},
		{name: "line filter mismatch", symbol: "Add", line: 100, wantErr: `"Add"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := edit.FindSymbol(path, tc.symbol, tc.line)
			require.Error(t, err)
			assert.ErrorIs(t, err, edit.ErrSymbolNotFound)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestApply_ReturnsError_When_RequestIsInvalid(t *testing.T) {
	t.Parallel()

	path := writeGoFile(t)
	tests := []struct {
		name    string
		req     edit.Request
		wantErr string
	}{
		{
			name:    "unknown operation",
			req:     edit.Request{File: path, Symbol: "Add", Operation: edit.Operation("unknown")},
			wantErr: "unknown operation",
		},
		{
			name:    "replace body on const",
			req:     edit.Request{File: path, Symbol: "Meaning", Operation: edit.OpReplaceBody, NewCode: "return 0"},
			wantErr: "replace_body only works on functions/methods",
		},
		{
			name:    "symbol not found",
			req:     edit.Request{File: path, Symbol: "Nope", Operation: edit.OpReplaceFull, NewCode: "func Nope() {}"},
			wantErr: `"Nope"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := edit.Apply(tc.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestApply_ReturnsEditedCode_When_UsingSupportedOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		req       edit.Request
		wantInNew string
		inspect   func(*testing.T, *edit.Result)
	}{
		{
			name:      "replace body adds braces",
			req:       edit.Request{Symbol: "Add", Operation: edit.OpReplaceBody, NewCode: "return a - b"},
			wantInNew: "{\nreturn a - b\n}",
			inspect: func(t *testing.T, got *edit.Result) {
				assert.Contains(t, got.OriginalCode, "return a + b")
				assert.False(t, got.Applied)
				assert.Greater(t, got.NewLineEnd, got.LineStart)
			},
		},
		{
			name:      "replace full rewrites symbol",
			req:       edit.Request{Symbol: "Add", Operation: edit.OpReplaceFull, NewCode: "func Add(a, b int) int {\n\treturn a*b\n}"},
			wantInNew: "func Add(a, b int) int",
			inspect: func(t *testing.T, got *edit.Result) {
				assert.Contains(t, got.NewCode, "return a*b")
				assert.Contains(t, got.Diff, "@@")
			},
		},
		{
			name:      "insert before prepends symbol",
			req:       edit.Request{Symbol: "Add", Operation: edit.OpInsertBefore, NewCode: "// inserted before"},
			wantInNew: "// inserted before",
		},
		{
			name:      "insert after appends symbol",
			req:       edit.Request{Symbol: "Add", Operation: edit.OpInsertAfter, NewCode: "func Sub(a, b int) int { return a - b }"},
			wantInNew: "func Sub(a, b int) int",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeGoFile(t)
			tc.req.File = path

			got, err := edit.Apply(tc.req)
			require.NoError(t, err)

			assert.Contains(t, got.NewCode, tc.wantInNew)
			assert.NotEmpty(t, got.Diff)
			assert.Equal(t, path, got.File)
			if tc.inspect != nil {
				tc.inspect(t, got)
			}
		})
	}
}

func TestApplyAndWrite_PersistsFormattedChanges_When_RequestIsValid(t *testing.T) {
	t.Parallel()

	path := writeGoFile(t)
	result, err := edit.ApplyAndWrite(edit.Request{
		File:      path,
		Symbol:    "Add",
		Operation: edit.OpReplaceBody,
		NewCode:   "return a - b",
	})
	require.NoError(t, err)
	require.True(t, result.Applied)

	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	contents := string(updated)

	assert.Contains(t, contents, "return a - b")
	assert.NotContains(t, contents, "return a + b")
	assert.Contains(t, result.Diff, "return a - b")
}

func TestSymbolInfo_HandlesOutOfRangePositions_When_SourceOffsetsAreInvalid(t *testing.T) {
	t.Parallel()

	info := &edit.SymbolInfo{Source: []byte("abc"), PosStart: 0, PosEnd: 10, BodyStart: 4, BodyEnd: 2}
	assert.Empty(t, info.OriginalCode())
	assert.Empty(t, info.BodyCode())

	valid := &edit.SymbolInfo{Source: []byte("abcdef"), PosStart: 2, PosEnd: 5, BodyStart: 2, BodyEnd: 5}
	want := "bcd"
	if diff := cmp.Diff(want, valid.OriginalCode()); diff != "" {
		t.Fatalf("original code mismatch (-want +got):\n%s", diff)
	}
	assert.Equal(t, valid.OriginalCode(), valid.BodyCode())
}

func TestFindSymbol_ReturnsWrappedError_When_FileCannotBeRead(t *testing.T) {
	t.Parallel()

	_, err := edit.FindSymbol(filepath.Join(t.TempDir(), "missing.go"), "X", 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, os.ErrNotExist))
	assert.Contains(t, err.Error(), "read file")
	assert.False(t, strings.Contains(err.Error(), "panic"))
}
