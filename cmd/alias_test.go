package cmd

import (
	"bytes"
	"testing"
)

func TestAliasNudge(t *testing.T) {
	var buf bytes.Buffer
	aliasNudge(&buf, "orient", "context --out")
	if got, want := buf.String(), "snipe: 'orient' is now 'context --out'\n"; got != want {
		t.Fatalf("aliasNudge = %q, want %q", got, want)
	}
}
