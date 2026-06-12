package cmd

import "testing"

func TestShouldHeal(t *testing.T) {
	cases := []struct {
		name    string
		changed int
		want    bool
	}{
		{"no drift", 0, false},
		{"one file", 1, true},
		{"at cap", healMaxChangedFiles, true},
		{"over cap warns instead", healMaxChangedFiles + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldHeal(tc.changed); got != tc.want {
				t.Errorf("shouldHeal(%d) = %v, want %v", tc.changed, got, tc.want)
			}
		})
	}
}
