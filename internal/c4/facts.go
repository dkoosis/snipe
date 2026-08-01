// Package c4 aggregates signals already indexed by snipe into a C4-style
// architecture fact inventory — containers, datastores, external systems, and
// the flows between them. It is a thin extraction/classification layer, not a
// new analysis engine: every fact traces to an allowlist, regex, or
// import-root match against real index data (symbols, imports, string_refs).
// No rendering happens here; consumers (the c4-model skill, transform:d2)
// render the facts this package emits.
package c4

import (
	"database/sql"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/graphmetrics"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
)

// componentTopN caps the --level component package breakdown to the top-N
// packages by import-graph PageRank (falls back to every package when
// graph_metrics hasn't been populated — see graphmetrics.TopNByPageRank).
const componentTopN = 20

// Level values for `snipe c4 --level`.
const (
	LevelContext   = "context"
	LevelContainer = "container"
	LevelComponent = "component"
)

// Facts is the C4 fact inventory for one repo at one --level.
type Facts struct {
	Module     string
	GoVersion  string
	Containers []Container
	Datastores []Datastore
	External   []ExternalSystem
	Flows      []Flow
	Components []Component
}

// Container is a deployable process — today, always a Go main package.
type Container struct {
	Name string
	Tech string
	File string
	Line int
}

// Datastore is a package that imports a driver from the fixed allowlist.
type Datastore struct {
	Name    string // display name, e.g. "SQLite"
	Driver  string // the matched import pkg_path
	Package string // the importing package
	File    string
	Line    int
}

// ExternalSystem is a third-party service, detected via env-var literals
// and/or third-party SDK imports.
type ExternalSystem struct {
	Name     string   // grouped display name, e.g. "VOYAGE"
	Kind     string   // "env" | "import" | "env+import"
	Evidence []string // env var names and/or importer package paths
	File     string
	Line     int
}

// Flow is a directed edge from an importer package to a datastore or
// external system, with one deterministic file:line evidence row.
type Flow struct {
	From string // importer package
	To   string // datastore or external system name
	Kind string // "datastore" | "external"
	File string
	Line int
}

// Component is a package-level breakdown entry for --level component.
type Component struct {
	Name    string
	Layer   string
	Purpose string
}

// driverAllowlist maps a substring match against imports.pkg_path to a
// display datastore name (design field's fixed allowlist). Order does not
// matter: matches are independent, evaluated against every import row.
var driverAllowlist = []struct{ match, name string }{
	{"modernc.org/sqlite", "SQLite"},
	{"mattn/go-sqlite3", "SQLite"},
	{"lib/pq", "PostgreSQL"},
	{"jackc/pgx", "PostgreSQL"},
	{"go-redis", "Redis"},
	{"bbolt", "BoltDB"},
	{"badger", "Badger"},
	{"database/sql", "SQL"},
}

// envSuffixRe matches the design field's external-system env-var predicate:
// value ends in _API_KEY, _TOKEN, _SECRET, _URL, or _MODEL.
var envSuffixRe = regexp.MustCompile(`(_API_KEY|_TOKEN|_SECRET|_URL|_MODEL)$`)

// envRoleTokens are the generic role words stripped from an env var name
// before grouping — they describe the credential's shape (API key, URL,
// model id), not which service it belongs to, so they never distinguish one
// external system from another.
var envRoleTokens = map[string]bool{
	"API":    true,
	"KEY":    true,
	"TOKEN":  true,
	"SECRET": true,
	"URL":    true,
	"MODEL":  true,
}

// testLibAllowlist excludes common test-only libraries from external-system
// import detection (confirmed real deps of snipe: go.mod lines for
// stretchr/testify and google/go-cmp).
var testLibAllowlist = []string{
	"github.com/stretchr/testify",
	"github.com/google/go-cmp",
}

// knownServiceAllowlist recognizes import roots that are themselves strong
// evidence of a runtime external system — well-known SaaS/service SDKs —
// even with no correlating env-var credential. A bare third-party import
// that matches neither this allowlist nor an env system (see
// mergeExternalSystems) is an ordinary compile-time library dependency (a
// CLI framework, a logging lib, ...), not a runtime external system: import
// evidence alone overclaims what is only a "linked at compile time" fact.
var knownServiceAllowlist = []string{
	"stripe",
	"twilio",
	"sendgrid",
	"openai",
	"anthropic",
	"aws/aws-sdk-go",
	"slack-go/slack",
	"plaid/plaid-go",
}

// isKnownService reports whether root matches the curated known-service
// allowlist.
func isKnownService(root string) bool {
	for _, s := range knownServiceAllowlist {
		if strings.Contains(root, s) {
			return true
		}
	}
	return false
}

// BuildFacts assembles the C4 fact inventory for the repo indexed at s, whose
// root is dir. level filters which sections populate:
//   - context:   module + external systems + datastores only, no containers
//   - container: containers + datastores + external systems + flows (default)
//   - component: container-level facts plus a package-level breakdown
func BuildFacts(s *store.Store, dir string, level string) (Facts, error) {
	db := s.DB()

	modulePath := query.DetectModulePath(db)
	f := Facts{Module: modulePath}
	if v, err := goVersionFromMod(dir); err == nil && v != "" {
		f.GoVersion = v
	}

	datastores, err := findDatastores(db)
	if err != nil {
		return Facts{}, fmt.Errorf("find datastores: %w", err)
	}
	f.Datastores = datastores

	external, err := findExternalSystems(db, modulePath)
	if err != nil {
		return Facts{}, fmt.Errorf("find external systems: %w", err)
	}
	f.External = external

	if level == LevelContext {
		return f, nil
	}

	containers, err := findContainers(db, modulePath)
	if err != nil {
		return Facts{}, fmt.Errorf("find containers: %w", err)
	}
	tech := "Go"
	if f.GoVersion != "" {
		tech = "Go " + f.GoVersion
	}
	for i := range containers {
		containers[i].Tech = tech
	}
	f.Containers = containers

	flows, err := findFlows(db, datastores, external)
	if err != nil {
		return Facts{}, fmt.Errorf("find flows: %w", err)
	}
	f.Flows = flows

	if level == LevelComponent {
		components, err := findComponents(s)
		if err != nil {
			return Facts{}, fmt.Errorf("find components: %w", err)
		}
		f.Components = components
	}

	return f, nil
}

// goVersionFromMod reads the `go` directive from dir/go.mod.
func goVersionFromMod(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return "", err
	}
	if mf.Go != nil {
		return mf.Go.Version, nil
	}
	return "", nil
}

// findContainers returns one Container per distinct package declaring a
// `func main()`, named after the module (the design field's AC expects a
// single "snipe" container on snipe's own single-binary repo).
func findContainers(db *sql.DB, modulePath string) ([]Container, error) {
	rows, err := db.Query(`
		SELECT DISTINCT file_path, line_start
		FROM symbols
		WHERE name = 'main' AND kind = 'func'
		ORDER BY file_path, line_start
	`)
	if err != nil {
		return nil, fmt.Errorf("query main funcs: %w", err)
	}
	defer rows.Close()

	name := moduleDisplayName(modulePath)
	var out []Container
	for rows.Next() {
		var file string
		var line int
		if err := rows.Scan(&file, &line); err != nil {
			return nil, fmt.Errorf("scan main func: %w", err)
		}
		out = append(out, Container{
			Name: name,
			File: file,
			Line: line,
		})
	}
	return out, rows.Err()
}

// moduleDisplayName returns the last path segment of a module path, e.g.
// "github.com/dkoosis/snipe" -> "snipe".
func moduleDisplayName(modulePath string) string {
	if modulePath == "" {
		return ""
	}
	parts := strings.Split(modulePath, "/")
	return parts[len(parts)-1]
}

// findDatastores scans the imports table for rows matching the fixed driver
// allowlist, deduped by (display name, importing package).
func findDatastores(db *sql.DB) ([]Datastore, error) {
	rows, err := db.Query(`
		SELECT file_path, pkg_path, COALESCE(importer_pkg,''), line
		FROM imports
		ORDER BY pkg_path, file_path, line
	`)
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer rows.Close()

	type key struct{ name, pkg string }
	seen := make(map[key]bool)
	var out []Datastore
	for rows.Next() {
		var file, pkgPath, importerPkg string
		var line int
		if err := rows.Scan(&file, &pkgPath, &importerPkg, &line); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		for _, d := range driverAllowlist {
			if !strings.Contains(pkgPath, d.match) {
				continue
			}
			k := key{d.name, importerPkg}
			if seen[k] {
				break
			}
			seen[k] = true
			out = append(out, Datastore{
				Name:    d.name,
				Driver:  pkgPath,
				Package: importerPkg,
				File:    file,
				Line:    line,
			})
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Package < out[j].Package
	})
	return out, nil
}

// findExternalSystems combines env-var evidence and third-party-import
// evidence into one deduped, merged list of external systems.
func findExternalSystems(db *sql.DB, modulePath string) ([]ExternalSystem, error) {
	envSystems, err := externalFromEnv(db, modulePath)
	if err != nil {
		return nil, err
	}
	importSystems, err := externalFromImports(db, modulePath)
	if err != nil {
		return nil, err
	}
	return mergeExternalSystems(envSystems, importSystems), nil
}

// externalFromEnv groups env-var literals matching envSuffixRe by a
// deterministic group key: the env var name with generic role tokens and the
// module's own name stripped. "VOYAGE_API_URL", "VOYAGE_MODEL", and
// "SNIPE_VOYAGE_API_KEY" all reduce to the key "VOYAGE" once role words
// (API/URL/MODEL/KEY) and the module token (SNIPE) are removed.
func externalFromEnv(db *sql.DB, modulePath string) ([]ExternalSystem, error) {
	lits, err := query.FindLiteralsByKind(db, "env")
	if err != nil {
		return nil, fmt.Errorf("find env literals: %w", err)
	}

	moduleToken := strings.ToUpper(moduleDisplayName(modulePath))

	type group struct {
		names []string
		file  string
		line  int
	}
	groups := make(map[string]*group)
	var order []string
	for i := range lits {
		l := &lits[i]
		if !envSuffixRe.MatchString(l.Value) {
			continue
		}
		key := envGroupKey(l.Value, moduleToken)
		g, ok := groups[key]
		if !ok {
			g = &group{file: l.FilePath, line: l.Line}
			groups[key] = g
			order = append(order, key)
		}
		g.names = append(g.names, l.Value)
		if isEarlierEvidence(l.FilePath, l.Line, g.file, g.line) {
			g.file, g.line = l.FilePath, l.Line
		}
	}

	sort.Strings(order)
	out := make([]ExternalSystem, 0, len(order))
	for _, key := range order {
		g := groups[key]
		evidence := append([]string{}, g.names...)
		sort.Strings(evidence)
		out = append(out, ExternalSystem{
			Name:     key,
			Kind:     "env",
			Evidence: evidence,
			File:     g.file,
			Line:     g.line,
		})
	}
	return out, nil
}

// envGroupKey strips generic role tokens and the project's own module-name
// token from an underscore-delimited env var name, returning what remains
// joined back with "_". An empty result (the whole name was role words)
// falls back to the original name so no env var is silently dropped.
func envGroupKey(name, moduleToken string) string {
	parts := strings.Split(name, "_")
	var kept []string
	for _, p := range parts {
		if p == "" || envRoleTokens[p] || (moduleToken != "" && p == moduleToken) {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return name
	}
	return strings.Join(kept, "_")
}

// externalFromImports finds third-party imports with a dot-domain root,
// excluding the repo's own module, stdlib, golang.org/x/*, test-only imports
// (every importing file is *_test.go), and the test-lib allowlist.
func externalFromImports(db *sql.DB, modulePath string) ([]ExternalSystem, error) {
	rows, err := db.Query(`
		SELECT file_path, pkg_path, line
		FROM imports
		ORDER BY pkg_path, file_path, line
	`)
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer rows.Close()

	byRoot := make(map[string][]string) // root -> importing file paths (with dupes)
	bestFile := make(map[string]string) // root -> lowest-sorted (file, line) evidence
	bestLine := make(map[string]int)
	var order []string
	for rows.Next() {
		var file, pkgPath string
		var line int
		if err := rows.Scan(&file, &pkgPath, &line); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		if isDriverMatch(pkgPath) {
			continue // classified as a datastore, not an external system
		}
		root := dotDomainRoot(pkgPath)
		if root == "" {
			continue // no dot-domain root: stdlib or relative
		}
		if modulePath != "" && strings.HasPrefix(pkgPath, modulePath) {
			continue // own module
		}
		if strings.HasPrefix(pkgPath, "golang.org/x/") {
			continue
		}
		if isTestLibAllowed(pkgPath) {
			continue
		}
		if _, seen := byRoot[root]; !seen {
			order = append(order, root)
		}
		byRoot[root] = append(byRoot[root], file)
		if isEarlierEvidence(file, line, bestFile[root], bestLine[root]) {
			bestFile[root] = file
			bestLine[root] = line
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Strings(order)
	out := make([]ExternalSystem, 0, len(order))
	for _, root := range order {
		occFiles := byRoot[root]
		if allTestFiles(occFiles) {
			continue // test-only import: excluded per design field
		}
		out = append(out, ExternalSystem{
			Name:     root,
			Kind:     "import",
			Evidence: []string{root},
			File:     bestFile[root],
			Line:     bestLine[root],
		})
	}
	return out, nil
}

func allTestFiles(files []string) bool {
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			return false
		}
	}
	return len(files) > 0
}

// isDriverMatch reports whether pkgPath matches the datastore driver
// allowlist — such imports are reported as datastores, not external systems.
func isDriverMatch(pkgPath string) bool {
	for _, d := range driverAllowlist {
		if strings.Contains(pkgPath, d.match) {
			return true
		}
	}
	return false
}

// isTestLibAllowed reports whether pkgPath is a known test-only library that
// should never surface as an external system regardless of import shape.
func isTestLibAllowed(pkgPath string) bool {
	for _, lib := range testLibAllowlist {
		if strings.HasPrefix(pkgPath, lib) {
			return true
		}
	}
	return false
}

// dotDomainRoot returns the module-root portion of a package path if its
// first path segment contains a dot (a real hostname, e.g. "github.com"),
// else "" (stdlib or relative path). For known multi-segment hosts
// (github.com, gitlab.com, bitbucket.org) the root is host/org/repo (3
// segments); otherwise it is host/repo (2 segments, e.g. "modernc.org/sqlite").
func dotDomainRoot(pkgPath string) string {
	parts := strings.Split(pkgPath, "/")
	if len(parts) == 0 || !strings.Contains(parts[0], ".") {
		return ""
	}
	switch parts[0] {
	case "github.com", "gitlab.com", "bitbucket.org":
		if len(parts) >= 3 {
			return strings.Join(parts[:3], "/")
		}
		return strings.Join(parts, "/")
	default:
		if len(parts) >= 2 {
			return strings.Join(parts[:2], "/")
		}
		return parts[0]
	}
}

// mergeExternalSystems folds an import-detected external system into its
// env-detected counterpart when their names correlate (e.g. env group
// "STRIPE" and import root "github.com/stripe/stripe-go", whose org segment
// uppercases to "STRIPE") — the design field's AC #3 wants ONE external
// system per real-world service, not one row per detection method.
func mergeExternalSystems(envSystems, importSystems []ExternalSystem) []ExternalSystem {
	usedImport := make([]bool, len(importSystems))
	merged := make([]ExternalSystem, 0, len(envSystems)+len(importSystems))

	for _, e := range envSystems {
		matchIdx := -1
		for i, im := range importSystems {
			if usedImport[i] {
				continue
			}
			token := importOrgToken(im.Name)
			if token == "" {
				continue
			}
			if token == e.Name {
				matchIdx = i
				break
			}
		}
		if matchIdx == -1 {
			merged = append(merged, e)
			continue
		}
		im := importSystems[matchIdx]
		usedImport[matchIdx] = true

		combined := e
		combined.Kind = "env+import"
		combined.Evidence = append(append([]string{}, e.Evidence...), im.Evidence...)
		if isEarlierEvidence(im.File, im.Line, combined.File, combined.Line) {
			combined.File, combined.Line = im.File, im.Line
		}
		merged = append(merged, combined)
	}

	for i, im := range importSystems {
		if usedImport[i] {
			continue
		}
		if !isKnownService(im.Name) {
			continue // bare import, no env correlation: compile-time dep, not a runtime external system
		}
		merged = append(merged, im)
	}

	sort.Slice(merged, func(i, j int) bool { return merged[i].Name < merged[j].Name })
	return merged
}

// importOrgToken extracts the organization/package segment from a module
// root (e.g. "github.com/stripe/stripe-go" -> "STRIPE", "modernc.org/sqlite"
// -> "SQLITE") for correlating against an env-group name.
func importOrgToken(moduleRoot string) string {
	parts := strings.Split(moduleRoot, "/")
	if len(parts) < 2 {
		return ""
	}
	seg := parts[1]
	if i := strings.IndexAny(seg, "-_"); i > 0 {
		seg = seg[:i]
	}
	return strings.ToUpper(seg)
}

// isEarlierEvidence reports whether (file, line) sorts before (bestFile,
// bestLine) — lowest-sorted file first, then lowest line — the design
// field's deterministic evidence-pick rule (no arbitrary "first match").
func isEarlierEvidence(file string, line int, bestFile string, bestLine int) bool {
	if bestFile == "" {
		return true
	}
	if file != bestFile {
		return file < bestFile
	}
	return line < bestLine
}

// findFlows builds one deterministic edge per (importer package, datastore or
// external system) pair already discovered by findDatastores/
// findExternalSystems, reusing their evidence rather than re-querying.
func findFlows(db *sql.DB, datastores []Datastore, external []ExternalSystem) ([]Flow, error) {
	var out []Flow
	for _, d := range datastores {
		if d.Package == "" {
			continue
		}
		out = append(out, Flow{From: d.Package, To: d.Name, Kind: "datastore", File: d.File, Line: d.Line})
	}

	importerByRoot, err := loadImporterPkgsByRoot(db)
	if err != nil {
		return nil, err
	}
	for _, e := range external {
		// The flow's importer-package lookup is keyed by the raw import
		// module root (e.g. "github.com/stripe/stripe-go"), but a merged
		// env+import system's Name is the env-group display name (e.g.
		// "STRIPE") instead — mergeExternalSystems keeps the original import
		// root as one of the Evidence entries precisely so this lookup can
		// still find it.
		importers := importerByRoot[e.Name]
		if len(importers) == 0 {
			for _, ev := range e.Evidence {
				if imp := importerByRoot[ev]; len(imp) > 0 {
					importers = imp
					break
				}
			}
		}
		if len(importers) == 0 {
			// env-only external system: no import-graph edge to attach, but
			// still surface a flow anchored on the module root itself so the
			// service isn't silently missing from the flow list.
			out = append(out, Flow{From: "", To: e.Name, Kind: "external", File: e.File, Line: e.Line})
			continue
		}
		for _, imp := range importers {
			out = append(out, Flow{From: imp.pkg, To: e.Name, Kind: "external", File: imp.file, Line: imp.line})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

type importerEvidence struct {
	pkg  string
	file string
	line int
}

// loadImporterPkgsByRoot maps a dot-domain module root to the deterministic
// (lowest file, lowest line) evidence per importing package.
func loadImporterPkgsByRoot(db *sql.DB) (map[string][]importerEvidence, error) {
	rows, err := db.Query(`
		SELECT file_path, pkg_path, COALESCE(importer_pkg,''), line
		FROM imports
		ORDER BY pkg_path, file_path, line
	`)
	if err != nil {
		return nil, fmt.Errorf("query imports: %w", err)
	}
	defer rows.Close()

	best := make(map[string]map[string]importerEvidence) // root -> importer pkg -> best evidence
	for rows.Next() {
		var file, pkgPath, importerPkg string
		var line int
		if err := rows.Scan(&file, &pkgPath, &importerPkg, &line); err != nil {
			return nil, fmt.Errorf("scan import: %w", err)
		}
		if importerPkg == "" {
			continue
		}
		root := dotDomainRoot(pkgPath)
		if root == "" {
			continue
		}
		if best[root] == nil {
			best[root] = make(map[string]importerEvidence)
		}
		cur, ok := best[root][importerPkg]
		if !ok || isEarlierEvidence(file, line, cur.file, cur.line) {
			best[root][importerPkg] = importerEvidence{pkg: importerPkg, file: file, line: line}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string][]importerEvidence, len(best))
	for root, byPkg := range best {
		list := make([]importerEvidence, 0, len(byPkg))
		for _, ev := range byPkg {
			list = append(list, ev)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].pkg < list[j].pkg })
		out[root] = list
	}
	return out, nil
}

// findComponents builds the --level component package-level breakdown:
// top-N packages by import-graph PageRank, grouped by internal layer, with a
// purpose line sourced only from a real package_docs doc comment (never a
// name-based guess — the design field's "zero-inference" constraint applies
// here too).
//
// layer/short-name grouping duplicates the ~15-line logic in cmd/diagram.go
// (layerOf/shortPkg) rather than reusing it: those helpers are unexported in
// package cmd, and internal/c4 cannot import cmd without an import cycle
// (cmd/c4.go imports internal/c4). The design field's file-list note
// authorizes this small duplication as the fallback when the package
// boundary blocks reuse.
func findComponents(s *store.Store) ([]Component, error) {
	g, err := graphmetrics.LoadImportsGraph(s)
	if err != nil {
		return nil, fmt.Errorf("load imports graph: %w", err)
	}

	keep, _, err := graphmetrics.TopNByPageRank(s, "imports", componentTopN)
	if err != nil {
		return nil, fmt.Errorf("top-N by pagerank: %w", err)
	}

	nodes := g.Nodes()
	if keep != nil {
		filtered := nodes[:0:0]
		for _, n := range nodes {
			if keep[n] {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}
	sort.Strings(nodes)

	module := query.DetectModulePath(s.DB())
	docs, err := loadPackageDocFirstSentences(s.DB(), nodes)
	if err != nil {
		return nil, fmt.Errorf("load package docs: %w", err)
	}

	out := make([]Component, 0, len(nodes))
	for _, pkg := range nodes {
		out = append(out, Component{
			Name:    componentShortPkg(pkg, module),
			Layer:   componentLayer(pkg, module),
			Purpose: docs[pkg],
		})
	}
	return out, nil
}

// loadPackageDocFirstSentences reads real doc comments from package_docs for
// the given packages, returning only the first sentence (via
// context.ExtractFirstSentence — no name-based purpose inference, unlike
// internal/context's getPackagePurposes, which falls back to a heuristic
// guess; that fallback is exactly the kind of "guessed description" the
// design field's zero-inference AC rules out).
func loadPackageDocFirstSentences(db *sql.DB, pkgs []string) (map[string]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(pkgs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(pkgs))
	for i, p := range pkgs {
		args[i] = p
	}
	// #nosec G201 -- placeholders are positional parameters
	q := "SELECT pkg_path, doc FROM package_docs WHERE pkg_path IN (" + placeholders + ")"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query package_docs: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(pkgs))
	for rows.Next() {
		var pkgPath, doc string
		if err := rows.Scan(&pkgPath, &doc); err != nil {
			return nil, fmt.Errorf("scan package_docs: %w", err)
		}
		out[pkgPath] = context.ExtractFirstSentence(doc)
	}
	return out, rows.Err()
}

// componentLayer mirrors cmd/diagram.go's layerOf: a coarse grouping label
// for a Go package path relative to the module root.
func componentLayer(pkg, module string) string {
	rest := strings.TrimPrefix(pkg, module)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "root"
	}
	parts := strings.Split(rest, "/")
	if parts[0] == "internal" && len(parts) >= 2 {
		return "internal/" + parts[1]
	}
	return parts[0]
}

// componentShortPkg mirrors cmd/diagram.go's shortPkg: the package path with
// the module prefix stripped.
func componentShortPkg(pkg, module string) string {
	rest := strings.TrimPrefix(pkg, module)
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return path.Base(pkg)
	}
	return rest
}
