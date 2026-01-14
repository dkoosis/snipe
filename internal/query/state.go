package query

import (
	"database/sql"
	"errors"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/output"
)

// CheckIndexState computes current fingerprint and compares with stored
func CheckIndexState(db *sql.DB, repoRoot, version string) output.IndexState {
	// Compute current fingerprint
	current, err := index.ComputeFingerprint(repoRoot, version)
	if err != nil {
		return output.IndexMissing
	}

	// Get stored fingerprint
	var stored string
	err = db.QueryRow(`SELECT value FROM meta WHERE key = 'fingerprint'`).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) || stored == "" {
		return output.IndexMissing
	}
	if err != nil {
		return output.IndexMissing
	}

	// Compare
	if current.Combined == stored {
		return output.IndexFresh
	}
	return output.IndexStale
}
