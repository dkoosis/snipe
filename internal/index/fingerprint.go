package index

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Fingerprint represents the state of a Go project for cache invalidation
type Fingerprint struct {
	Version  string // snipe version
	GoMod    string // hash of go.mod
	GoSum    string // hash of go.sum
	GoWork   string // hash of go.work (if present)
	GoEnv    string // relevant go env values
	Combined string // combined hash for quick comparison
}

// ComputeFingerprint computes the fingerprint for a Go project
func ComputeFingerprint(dir, version string) (*Fingerprint, error) {
	fp := &Fingerprint{Version: version}

	// Hash go.mod
	if h, err := HashFileSHA256(filepath.Join(dir, "go.mod")); err == nil {
		fp.GoMod = h
	}

	// Hash go.sum
	if h, err := HashFileSHA256(filepath.Join(dir, "go.sum")); err == nil {
		fp.GoSum = h
	}

	// Hash go.work (optional)
	if h, err := HashFileSHA256(filepath.Join(dir, "go.work")); err == nil {
		fp.GoWork = h
	}

	// Get relevant go env values
	fp.GoEnv = getGoEnvHash()

	// Compute combined hash
	fp.Combined = computeCombinedHash(fp)

	return fp, nil
}

func getGoEnvHash() string {
	// Only include build-config values that affect type-checking output.
	// These are standard env vars readable directly — no need to shell out
	// to `go env`, which costs 50-200ms and runs on every query-time
	// staleness check.
	envVars := []string{"CGO_ENABLED", "GOARCH", "GOOS"}

	var values []string
	for _, key := range envVars {
		if val := os.Getenv(key); val != "" {
			values = append(values, key+"="+val)
		}
	}

	sort.Strings(values)
	h := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return hex.EncodeToString(h[:8])
}

func computeCombinedHash(fp *Fingerprint) string {
	// Version intentionally excluded — a snipe rebuild should not
	// invalidate the index of every target repo.  Only dependency
	// and toolchain changes (go.mod, go.sum, go.work, go env) matter.
	data := strings.Join([]string{
		fp.GoMod,
		fp.GoSum,
		fp.GoWork,
		fp.GoEnv,
	}, "|")

	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:8])
}

// String returns a string representation of the fingerprint
func (fp *Fingerprint) String() string {
	return fp.Combined
}
