package risk

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RecordFile is where each answer is kept, under the repo's .snipe dir.
const RecordFile = "risk.jsonl"

// Record is one snipe risk answer as it was given: the refs asked about, the
// commits they named then, and every measure. snipe keeps what it said so its
// weights can later be judged against sdlc's review outcomes (sn-qtjl). It is
// a separate file from usage.jsonl because the measures are too large for a
// usage row and have a different reader.
type Record struct {
	TS         string `json:"ts"`
	Base       string `json:"base"`
	Head       string `json:"head"`
	BaseSHA    string `json:"base_sha"` // empty when the ref did not resolve
	HeadSHA    string `json:"head_sha"`
	IndexState string `json:"index_state"`
	Measures
}

// Append adds m as one line of <root>/.snipe/risk.jsonl. It writes only where
// .snipe already exists, so a run on an unindexed repo leaves no trace, and
// it honors SNIPE_NO_TELEMETRY. Failures are silent: keeping the record must
// never break the answer.
func Append(root, base, head, indexState string, m Measures) {
	if root == "" || os.Getenv("SNIPE_NO_TELEMETRY") != "" {
		return
	}
	dir := filepath.Join(root, ".snipe")
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return
	}
	rec := Record{
		TS:         time.Now().Format(time.RFC3339),
		Base:       base,
		Head:       head,
		BaseSHA:    revParse(root, base),
		HeadSHA:    revParse(root, head),
		IndexState: indexState,
		Measures:   m,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	fh, err := os.OpenFile(filepath.Join(dir, RecordFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G304 -- path derived from project root
	if err != nil {
		return
	}
	defer fh.Close()
	_, _ = fh.Write(append(line, '\n'))
}

// revParse names the commit ref points at, or "" when it does not resolve.
func revParse(root, ref string) string {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return ""
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet",
		"--end-of-options", ref+"^{commit}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
