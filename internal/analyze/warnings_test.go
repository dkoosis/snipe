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

func TestDetectDeferInLoop(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantWarn bool
	}{
		{
			name: "defer in for loop",
			code: `package test
func bad() {
	for i := 0; i < 10; i++ {
		defer println(i)
	}
}`,
			wantWarn: true,
		},
		{
			name: "defer in range loop",
			code: `package test
func bad(files []string) {
	for _, name := range files {
		defer println(name)
	}
}`,
			wantWarn: true,
		},
		{
			name: "defer outside loop",
			code: `package test
func good() {
	defer println("done")
	for i := 0; i < 10; i++ {
		println(i)
	}
}`,
			wantWarn: false,
		},
		{
			name: "no defer",
			code: `package test
func simple() {
	for i := 0; i < 10; i++ {
		println(i)
	}
}`,
			wantWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			analyzer := NewAnalyzer(fset, []byte(tt.code), output.WarningsFull)

			var foundWarn bool
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					warnings := analyzer.AnalyzeFunc(fn)
					for _, w := range warnings {
						if w.Code == output.WarnDeferInLoop {
							foundWarn = true
						}
					}
				}
			}

			if tt.wantWarn {
				assert.True(t, foundWarn, "expected defer_in_loop warning")
			} else {
				assert.False(t, foundWarn, "unexpected defer_in_loop warning")
			}
		})
	}
}

func TestDetectIgnoredError(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantWarn bool
	}{
		{
			name: "blank identifier on call",
			code: `package test
func bad() {
	_ = doSomething()
}
func doSomething() error { return nil }`,
			wantWarn: true,
		},
		{
			name: "ignored fmt.Println",
			code: `package test
import "fmt"
func ok() {
	_ = fmt.Println("hello")
}`,
			wantWarn: false,
		},
		{
			name: "error properly handled",
			code: `package test
func good() error {
	err := doSomething()
	return err
}
func doSomething() error { return nil }`,
			wantWarn: false,
		},
		{
			name: "no blank identifier",
			code: `package test
func simple() {
	x := 1
	println(x)
}`,
			wantWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			analyzer := NewAnalyzer(fset, []byte(tt.code), output.WarningsFull)

			var foundWarn bool
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					warnings := analyzer.AnalyzeFunc(fn)
					for _, w := range warnings {
						if w.Code == output.WarnIgnoredError {
							foundWarn = true
						}
					}
				}
			}

			if tt.wantWarn {
				assert.True(t, foundWarn, "expected ignored_error warning")
			} else {
				assert.False(t, foundWarn, "unexpected ignored_error warning")
			}
		})
	}
}

func TestDetectLostCancel(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantWarn bool
	}{
		{
			name: "WithCancel without defer",
			code: `package test
import "context"
func bad(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	_ = ctx
	_ = cancel
}`,
			wantWarn: true,
		},
		{
			name: "WithCancel with defer cancel",
			code: `package test
import "context"
func good(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_ = ctx
}`,
			wantWarn: false,
		},
		{
			name: "cancel explicitly ignored with blank",
			code: `package test
import "context"
func explicit(ctx context.Context) {
	ctx, _ = context.WithCancel(ctx)
	_ = ctx
}`,
			wantWarn: false,
		},
		{
			name: "no context usage",
			code: `package test
func simple() {
	x := 1
	_ = x
}`,
			wantWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			require.NoError(t, err)

			analyzer := NewAnalyzer(fset, []byte(tt.code), output.WarningsFull)

			var foundWarn bool
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok {
					warnings := analyzer.AnalyzeFunc(fn)
					for _, w := range warnings {
						if w.Code == output.WarnLostCancel {
							foundWarn = true
						}
					}
				}
			}

			if tt.wantWarn {
				assert.True(t, foundWarn, "expected lost_cancel warning")
			} else {
				assert.False(t, foundWarn, "unexpected lost_cancel warning")
			}
		})
	}
}

func TestWarningsModeNone(t *testing.T) {
	code := `package test
func bad() {
	for i := 0; i < 10; i++ {
		defer println(i)
	}
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	require.NoError(t, err)

	analyzer := NewAnalyzer(fset, []byte(code), output.WarningsNone)

	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			warnings := analyzer.AnalyzeFunc(fn)
			assert.Empty(t, warnings, "WarningsNone should produce no warnings")
		}
	}
}

func TestWarningsModeFast(t *testing.T) {
	code := `package test
import "context"
func test(ctx context.Context) {
	for i := 0; i < 10; i++ {
		defer println(i)
	}
	ctx, cancel := context.WithCancel(ctx)
	_ = ctx
	_ = cancel
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", code, 0)
	require.NoError(t, err)

	analyzer := NewAnalyzer(fset, []byte(code), output.WarningsFast)

	var foundDefer, foundCancel bool
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			warnings := analyzer.AnalyzeFunc(fn)
			for _, w := range warnings {
				if w.Code == output.WarnDeferInLoop {
					foundDefer = true
				}
				if w.Code == output.WarnLostCancel {
					foundCancel = true
				}
			}
		}
	}

	assert.True(t, foundDefer, "fast mode should detect defer_in_loop")
	assert.False(t, foundCancel, "fast mode should NOT detect lost_cancel")
}
