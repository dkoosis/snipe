package query

import "testing"

func TestParseSignatureCounts(t *testing.T) {
	tests := []struct {
		name     string
		sig      string
		params   int
		results  int
		variadic bool
	}{
		{
			name:     "simple function no results",
			sig:      "func Foo()",
			params:   0,
			results:  0,
			variadic: false,
		},
		{
			name:     "one param no results",
			sig:      "func Foo(x int)",
			params:   1,
			results:  0,
			variadic: false,
		},
		{
			name:     "two params one result",
			sig:      "func Add(a int, b int) int",
			params:   2,
			results:  1,
			variadic: false,
		},
		{
			name:     "variadic function",
			sig:      "func Printf(format string, args ...interface{}) (int, error)",
			params:   2,
			results:  2,
			variadic: true,
		},
		{
			name:     "method with receiver",
			sig:      "func (*Writer) WriteError(cmd string, err *Error) error",
			params:   2,
			results:  1,
			variadic: false,
		},
		{
			name:     "method with value receiver",
			sig:      "func (r Reader) Read(p []byte) (n int, err error)",
			params:   1,
			results:  2,
			variadic: false,
		},
		{
			name:     "complex types in params",
			sig:      "func Process(cfg map[string]interface{}, opts ...Option) (*Result, error)",
			params:   2,
			results:  2,
			variadic: true,
		},
		{
			name:     "generic function",
			sig:      "func Map[T, U any](s []T, f func(T) U) []U",
			params:   2,
			results:  1,
			variadic: false,
		},
		{
			name:     "function type param",
			sig:      "func Apply(fn func(int, int) int, a, b int) int",
			params:   3,
			results:  1,
			variadic: false,
		},
		{
			name:     "no func prefix",
			sig:      "Load(cfg LoadConfig) (*LoadResult, error)",
			params:   1,
			results:  2,
			variadic: false,
		},
		{
			name:     "empty string",
			sig:      "",
			params:   0,
			results:  0,
			variadic: false,
		},
		{
			name:     "multiple named results",
			sig:      "func Query(q string) (rows []Row, total int, err error)",
			params:   1,
			results:  3,
			variadic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, results, variadic := parseSignatureCounts(tt.sig)
			if params != tt.params {
				t.Errorf("params = %d, want %d", params, tt.params)
			}
			if results != tt.results {
				t.Errorf("results = %d, want %d", results, tt.results)
			}
			if variadic != tt.variadic {
				t.Errorf("variadic = %v, want %v", variadic, tt.variadic)
			}
		})
	}
}

func TestFindMatchingParen(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		start int
		want  int
	}{
		{"simple", "()", 0, 1},
		{"nested", "(())", 0, 3},
		{"deep nested", "((()))", 0, 5},
		{"with content", "(a, b)", 0, 5},
		{"nested with content", "(fn(x, y) int)", 0, 13},
		{"no opening paren", "abc", 0, -1},
		{"start out of bounds", "()", 5, -1},
		{"unmatched", "((()", 0, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMatchingParen(tt.s, tt.start)
			if got != tt.want {
				t.Errorf("findMatchingParen(%q, %d) = %d, want %d", tt.s, tt.start, got, tt.want)
			}
		})
	}
}

func TestCountParams(t *testing.T) {
	tests := []struct {
		name     string
		paramStr string
		count    int
		variadic bool
	}{
		{"empty", "", 0, false},
		{"single param", "x int", 1, false},
		{"two params same type", "a, b int", 2, false},
		{"two params diff type", "a int, b string", 2, false},
		{"variadic", "args ...string", 1, true},
		{"mixed with variadic", "format string, args ...interface{}", 2, true},
		{"function param", "fn func(int, int) int", 1, false},
		{"map param", "m map[string]int", 1, false},
		{"slice param", "s []int", 1, false},
		{"generic param", "items []T", 1, false},
		{"complex nested", "m map[string]func(int) error, opts ...Option", 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, variadic := countParams(tt.paramStr)
			if count != tt.count {
				t.Errorf("count = %d, want %d", count, tt.count)
			}
			if variadic != tt.variadic {
				t.Errorf("variadic = %v, want %v", variadic, tt.variadic)
			}
		})
	}
}
