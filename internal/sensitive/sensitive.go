// Package sensitive classifies files into security-sensitive zones —
// authentication, cryptography, schema migrations, secrets, payments.
//
// Tornhill (Your Code as a Crime Scene) notes that some code is always
// significant regardless of size, churn, or complexity: a 20-line auth check
// matters more than a 2000-line churning UI file because a defect's cost is
// decoupled from the file's metrics. Flagging these zones keeps risk ranking
// from under-weighting small-but-critical code, mirroring the intent of
// GitHub CODEOWNERS path ownership.
//
// Classification has two layers: zero-config built-in heuristics over path
// and file names, plus an optional .snipe/sensitive file of CODEOWNERS-style
// "glob zone" rules that extend or add to the defaults.
package sensitive

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Zone is a category of always-significant code.
type Zone string

const (
	ZoneAuth      Zone = "auth"
	ZoneCrypto    Zone = "crypto"
	ZoneMigration Zone = "migration"
	ZoneSecret    Zone = "secret"
	ZonePayment   Zone = "payment"
)

// builtinKeywords maps a lower-cased substring (matched against the path) to
// the zone it implies. Ordered scanning is fine; matches accumulate.
var builtinKeywords = []struct {
	kw   string
	zone Zone
}{
	{"auth", ZoneAuth}, {"login", ZoneAuth}, {"session", ZoneAuth},
	{"oauth", ZoneAuth}, {"jwt", ZoneAuth}, {"credential", ZoneAuth},
	{"crypto", ZoneCrypto}, {"cipher", ZoneCrypto}, {"encrypt", ZoneCrypto},
	{"decrypt", ZoneCrypto}, {"x509", ZoneCrypto}, {"tls", ZoneCrypto},
	{"migration", ZoneMigration}, {"migrate", ZoneMigration}, {"schema", ZoneMigration},
	{"secret", ZoneSecret}, {"password", ZoneSecret}, {"apikey", ZoneSecret},
	{"vault", ZoneSecret}, {"token", ZoneSecret},
	{"payment", ZonePayment}, {"billing", ZonePayment}, {"invoice", ZonePayment},
	{"charge", ZonePayment},
}

// rule is one parsed line from .snipe/sensitive.
type rule struct {
	glob string
	zone Zone
}

// Classifier assigns zones to repo-relative paths.
type Classifier struct {
	rules []rule // user-supplied globs (may be empty)
}

// Load reads .snipe/sensitive from repoRoot if present. A missing file is
// fine — the classifier still applies built-in heuristics. A malformed line
// is skipped rather than failing the whole load.
func Load(repoRoot string) *Classifier {
	c := &Classifier{}
	f, err := os.Open(filepath.Join(repoRoot, ".snipe", "sensitive"))
	if err != nil {
		return c
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		c.rules = append(c.rules, rule{glob: fields[0], zone: Zone(fields[1])})
	}
	return c
}

// Zones returns the sorted, de-duplicated zones for a repo-relative path,
// combining built-in keyword heuristics with any matching config globs.
func (c *Classifier) Zones(path string) []Zone {
	lower := strings.ToLower(path)
	set := make(map[Zone]struct{})
	for _, kw := range builtinKeywords {
		if strings.Contains(lower, kw.kw) {
			set[kw.zone] = struct{}{}
		}
	}
	for _, r := range c.rules {
		if matchGlob(r.glob, path) {
			set[r.zone] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]Zone, 0, len(set))
	for z := range set {
		out = append(out, z)
	}
	slices.Sort(out)
	return out
}

// matchGlob supports a leading/trailing "**" for recursive matching plus the
// single-segment semantics of filepath.Match. "internal/auth/**" matches any
// path under internal/auth; "*.sql" matches by base name.
func matchGlob(glob, path string) bool {
	if strings.HasSuffix(glob, "/**") {
		return strings.HasPrefix(path, strings.TrimSuffix(glob, "**"))
	}
	if suffix, ok := strings.CutPrefix(glob, "**/"); ok {
		return strings.HasSuffix(path, suffix) || strings.Contains(path, "/"+suffix)
	}
	if ok, _ := filepath.Match(glob, path); ok {
		return true
	}
	// Also try matching against the base name so "*.sql" works at any depth.
	ok, _ := filepath.Match(glob, filepath.Base(path))
	return ok
}
