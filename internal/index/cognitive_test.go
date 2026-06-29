package index

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// cognitiveOf parses a single Go func and returns its cognitive complexity.
func cognitiveOf(t *testing.T, src string) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package p\n"+src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return computeCognitive(fn.Body)
		}
	}
	t.Fatal("no func found")
	return -1
}

func TestCognitiveWhitePaperExamples(t *testing.T) {
	tests := []struct {
		name string
		want int
		src  string
	}{
		{
			// SonarSource white-paper canonical example: nesting drives the score.
			// for(+1) > for(+2) > if(+3) > continue OUTER(+1) = 7.
			name: "sumOfPrimes",
			want: 7,
			src: `func sumOfPrimes(max int) int {
				total := 0
			OUTER:
				for i := 1; i <= max; i++ {
					for j := 2; j < i; j++ {
						if i%j == 0 {
							continue OUTER
						}
					}
					total += i
				}
				return total
			}`,
		},
		{
			// A switch counts once, no matter how many cases — the white paper's
			// contrast case to cyclomatic complexity (which would score ~4 here).
			name: "switchCountsOnce",
			want: 1,
			src: `func words(n int) string {
				switch n {
				case 1:
					return "one"
				case 2:
					return "a couple"
				default:
					return "lots"
				}
			}`,
		},
		{
			// if/else-if/else: each takes a flat +1, no nesting penalty.
			name: "ifElseChain",
			want: 3,
			src: `func grade(n int) string {
				if n > 90 {
					return "A"
				} else if n > 80 {
					return "B"
				} else {
					return "C"
				}
			}`,
		},
		{
			// Boolean sequences: one run of && = +1 on top of the if's +1.
			name: "booleanSequence",
			want: 2,
			src: `func ok(a, b, c bool) bool {
				if a && b && c {
					return true
				}
				return false
			}`,
		},
		{
			// Alternating operators count as two sequences: if(+1) + && (+1) + || (+1).
			name: "booleanAlternation",
			want: 3,
			src: `func ok(a, b, c bool) bool {
				if a && b || c {
					return true
				}
				return false
			}`,
		},
		{
			// Linear code with no control flow → 0.
			name: "linear",
			want: 0,
			src: `func add(a, b int) int {
				s := a + b
				return s
			}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cognitiveOf(t, tt.src); got != tt.want {
				t.Errorf("cognitive(%s) = %d, want %d", tt.name, got, tt.want)
			}
		})
	}
}

func TestCognitiveNilBody(t *testing.T) {
	if got := computeCognitive(nil); got != 0 {
		t.Errorf("nil body = %d, want 0", got)
	}
}
