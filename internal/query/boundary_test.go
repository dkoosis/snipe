package query

import (
	"reflect"
	"sort"
	"testing"
)

func TestMatchPackagePatterns(t *testing.T) {
	universe := []string{
		"github.com/x/proj/internal/store",
		"github.com/x/proj/internal/store/migrations",
		"github.com/x/proj/internal/query",
		"github.com/x/proj/internal/query/resolve",
		"github.com/x/proj/cmd",
	}

	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{
			name:     "exact suffix match — single package",
			patterns: []string{"internal/store"},
			want:     []string{"github.com/x/proj/internal/store"},
		},
		{
			name:     "recursive suffix — package and descendants",
			patterns: []string{"internal/store/..."},
			want: []string{
				"github.com/x/proj/internal/store",
				"github.com/x/proj/internal/store/migrations",
			},
		},
		{
			name:     "multiple patterns union",
			patterns: []string{"internal/store", "cmd"},
			want: []string{
				"github.com/x/proj/cmd",
				"github.com/x/proj/internal/store",
			},
		},
		{
			name:     "no match returns empty",
			patterns: []string{"internal/notreal"},
			want:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchPackagePatterns(universe, tc.patterns)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
