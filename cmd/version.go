package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/dkoosis/snipe/internal/protocol"
)

var (
	Version   = "0.1.0"
	GitCommit = "unknown"
)

var versionJSON bool

// Features lists all snipe subcommands available for LLM callers.
// Hardcoded for stability.
var Features = []string{
	cmdNameDef, cmdNameRefs, cmdNameCallers, cmdNameCallees, cmdNameSearch,
	"context", cmdNameIndex,
	cmdNameSim, cmdNameTypes, cmdNameImpl, cmdNameDeps,
	cmdNamePkg, cmdNameEdit, cmdNamePack,
}

func runVersion() {
	if versionJSON {
		info := protocol.VersionInfo{
			Version:  Version,
			Protocol: protocol.ProtocolVersion,
			Features: Features,
			Commit:   GitCommit,
		}
		enc := json.NewEncoder(os.Stdout)
		if err := enc.Encode(info); err != nil {
			fmt.Fprintf(os.Stderr, "snipe version: write: %v\n", err)
		}
	} else {
		fmt.Printf("snipe version %s (commit: %s)\n", Version, GitCommit)
	}
}
