package cmd

import (
	"fmt"
	"io"
)

// Hidden aliases: an old verb stays runnable as a hidden kong command that
// prints one stderr nudge and delegates to the new path. A later fold adds one
// struct (tagged hidden:"") whose Run calls aliasNudge and then the new
// command's Run, plus one field in CLI.

// aliasNudge writes the single deprecation line for an alias to w (stderr in
// production, so stdout and --format json output stay those of the new command).
func aliasNudge(w io.Writer, oldVerb, newVerb string) {
	_, _ = fmt.Fprintf(w, "snipe: '%s' is now '%s'\n", oldVerb, newVerb)
}
