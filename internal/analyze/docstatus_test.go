package analyze

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dkoosis/snipe/internal/output"
)

func TestCheckDocStatus(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		doc        string
		wantStatus output.DocStatusCode
		wantReason bool
	}{
		{
			name: "fresh - doc matches signature",
			code: `package test
func Process(ctx context.Context, data []byte) error {
	return nil
}`,
			doc:        "Process handles incoming requests and validates them.",
			wantStatus: output.DocFresh,
			wantReason: false,
		},
		{
			name: "missing - no doc",
			code: `package test
func NoDoc() {}`,
			doc:        "",
			wantStatus: output.DocMissing,
			wantReason: false,
		},
		{
			name: "stale - doc references old param",
			code: `package test
func Updated(newParam string) {}`,
			doc:        "Updated uses the `oldParam` parameter to process data.",
			wantStatus: output.DocStale,
			wantReason: true,
		},
		{
			name: "fresh - backtick param matches",
			code: `package test
func WithParam(config Config) error {
	return nil
}`,
			doc:        "WithParam uses `config` to configure the operation.",
			wantStatus: output.DocFresh,
			wantReason: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			var fn *ast.FuncDecl
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					fn = fd
					break
				}
			}
			require.NotNil(t, fn)

			status := CheckDocStatus(fn, tt.doc)

			assert.Equal(t, tt.wantStatus, status.Status)
			if tt.wantReason {
				assert.NotEmpty(t, status.Reasons, "expected reasons for status")
			}
		})
	}
}

func TestExtractPurpose(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		doc          string
		wantSource   output.PurposeSource
		wantNonEmpty bool
	}{
		{
			name: "purpose from doc",
			code: `package test
func Calculate(x int) int { return x * 2 }`,
			doc:          "Calculate doubles the input value.",
			wantSource:   output.PurposeFromDoc,
			wantNonEmpty: true,
		},
		{
			name: "purpose from template - New prefix",
			code: `package test
func NewServer(config Config) *Server { return nil }`,
			doc:          "",
			wantSource:   output.PurposeFromTemplate,
			wantNonEmpty: true,
		},
		{
			name: "purpose from template - Get prefix",
			code: `package test
func GetUser(id string) *User { return nil }`,
			doc:          "",
			wantSource:   output.PurposeFromTemplate,
			wantNonEmpty: true,
		},
		{
			name: "purpose from template - method",
			code: `package test
func (s *Server) Shutdown() {}`,
			doc:          "",
			wantSource:   output.PurposeFromTemplate,
			wantNonEmpty: true,
		},
		{
			name: "purpose missing - no doc, no pattern",
			code: `package test
func xyz() {}`,
			doc:          "",
			wantSource:   output.PurposeMissing,
			wantNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			var fn *ast.FuncDecl
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					fn = fd
					break
				}
			}
			require.NotNil(t, fn)

			purpose, source := ExtractPurpose(fn, tt.doc)

			assert.Equal(t, tt.wantSource, source)
			if tt.wantNonEmpty {
				assert.NotEmpty(t, purpose, "expected non-empty purpose")
			}
		})
	}
}

func TestExtractFirstSentence(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "simple sentence",
			doc:  "Process handles the data.",
			want: "Process handles the data.",
		},
		{
			name: "multiple sentences",
			doc:  "Process handles the data. It also validates input.",
			want: "Process handles the data.",
		},
		{
			name: "sentence with newline",
			doc:  "Process handles the data.\nMore details here.",
			want: "Process handles the data.",
		},
		{
			name: "no sentence end",
			doc:  "Process handles the data",
			want: "Process handles the data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstSentence(tt.doc)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGeneratePurposeTemplate(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
		code     string
		want     string
	}{
		{
			name:     "New prefix",
			funcName: "NewServer",
			code:     `package test; func NewServer() {}`,
			want:     "Creates a new server.",
		},
		{
			name:     "Get prefix",
			funcName: "GetUser",
			code:     `package test; func GetUser() {}`,
			want:     "Retrieves user.",
		},
		{
			name:     "Is prefix",
			funcName: "IsValid",
			code:     `package test; func IsValid() {}`,
			want:     "Checks if valid.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			var fn *ast.FuncDecl
			for _, decl := range f.Decls {
				if fd, ok := decl.(*ast.FuncDecl); ok {
					fn = fd
					break
				}
			}
			require.NotNil(t, fn)

			got := generatePurposeTemplate(fn)
			assert.Equal(t, tt.want, got)
		})
	}
}
