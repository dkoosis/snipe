package context

import (
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Package directory constants.
const (
	pkgCmd      = "cmd"
	pkgQuery    = "query"
	pkgInternal = "internal"
)

// GenerateConfig configures context generation.
type GenerateConfig struct {
	// RepoRoot is the absolute path to the repository root
	RepoRoot string
	// DB is the snipe index database
	DB *sql.DB
	// Full includes all symbols, not just key ones
	Full bool
	// MaxSymbols is the maximum number of symbols to include per category (default: 20)
	MaxSymbols int
}

// Generate creates a ProjectContext from the snipe index.
func Generate(cfg GenerateConfig) (*ProjectContext, error) {
	if cfg.MaxSymbols == 0 {
		cfg.MaxSymbols = 20
	}

	ctx := &ProjectContext{
		Project:      generateProject(cfg.RepoRoot),
		Architecture: generateArchitecture(cfg.DB, cfg.RepoRoot),
		Files:        generateFiles(cfg.DB, cfg.RepoRoot),
		Symbols:      generateSymbols(cfg.DB, cfg.RepoRoot, cfg.Full, cfg.MaxSymbols),
		Meta:         generateMeta(cfg.DB),
	}

	return ctx, nil
}

func generateProject(repoRoot string) Project {
	name := filepath.Base(repoRoot)
	proj := Project{
		Name: name,
		Root: repoRoot,
		Lang: []string{"go"}, // snipe currently only supports Go
	}

	// Detect build system
	switch {
	case fileExists(filepath.Join(repoRoot, "magefile.go")):
		proj.Build = "mage"
		proj.Test = "mage test"
	case fileExists(filepath.Join(repoRoot, "Makefile")):
		proj.Build = "make"
		proj.Test = "make test"
	default:
		proj.Build = "go build ./..."
		proj.Test = "go test ./..."
	}

	return proj
}

func generateArchitecture(db *sql.DB, repoRoot string) Architecture {
	arch := Architecture{
		Components: []Component{},
		DataFlows:  []string{},
	}

	// Find top-level packages and their purposes
	packages := getPackageInfo(db, repoRoot)
	for _, pkg := range packages {
		comp := Component{
			Name:     pkg.name,
			Purpose:  pkg.purpose,
			Entry:    pkg.entry,
			KeyFiles: pkg.keyFiles,
		}
		arch.Components = append(arch.Components, comp)
	}

	// Generate data flow if cmd package exists
	hasCmd := false
	hasStore := false
	hasQuery := false
	for _, pkg := range packages {
		switch pkg.name {
		case pkgCmd:
			hasCmd = true
		case "store":
			hasStore = true
		case pkgQuery:
			hasQuery = true
		}
	}
	if hasCmd && hasStore {
		if hasQuery {
			arch.DataFlows = append(arch.DataFlows, "CLI -> query -> store -> SQLite")
		} else {
			arch.DataFlows = append(arch.DataFlows, "CLI -> store -> SQLite")
		}
	}

	return arch
}

type packageInfo struct {
	name     string
	purpose  string
	entry    string
	keyFiles []string
}

func getPackageInfo(db *sql.DB, repoRoot string) []packageInfo {
	// Query for distinct directories containing Go files
	rows, err := db.Query(`
		SELECT DISTINCT
			SUBSTR(file_path, LENGTH(?) + 2,
				INSTR(SUBSTR(file_path, LENGTH(?) + 2), '/') - 1
			) as pkg_dir
		FROM symbols
		WHERE file_path LIKE ? || '/%'
		ORDER BY pkg_dir
	`, repoRoot, repoRoot, repoRoot)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var packages []packageInfo
	seenDirs := make(map[string]bool)

	for rows.Next() {
		var dir sql.NullString
		if err := rows.Scan(&dir); err != nil || !dir.Valid || dir.String == "" {
			continue
		}
		pkgDir := dir.String
		if seenDirs[pkgDir] {
			continue
		}
		seenDirs[pkgDir] = true

		// Determine purpose based on directory name
		purpose := inferPurpose(pkgDir)

		// Find entry point
		entry := ""
		keyFiles := []string{}
		switch pkgDir {
		case pkgCmd:
			entry = "cmd/root.go"
			keyFiles = append(keyFiles, "cmd/*.go")
		case pkgInternal:
			// Get subdirectories
			keyFiles = append(keyFiles, "internal/*/*.go")
		}

		packages = append(packages, packageInfo{
			name:     pkgDir,
			purpose:  purpose,
			entry:    entry,
			keyFiles: keyFiles,
		})
	}

	return packages
}

func inferPurpose(dir string) string {
	purposes := map[string]string{
		"cmd":      "Command-line interface",
		"internal": "Internal implementation packages",
		"pkg":      "Public packages",
		"api":      "API definitions",
		"store":    "Data storage and persistence",
		"query":    "Query execution",
		"index":    "Code indexing",
		"output":   "Output formatting",
		"config":   "Configuration management",
		"search":   "Search functionality",
		"embed":    "Embedding/vector operations",
		"util":     "Utility functions",
		"test":     "Test utilities",
	}
	if purpose, ok := purposes[dir]; ok {
		return purpose
	}
	return "Application logic"
}

func generateFiles(db *sql.DB, repoRoot string) Files {
	files := Files{
		ByConcern: make(map[string]map[string]string),
	}

	// Query for files grouped by directory
	rows, err := db.Query(`
		SELECT DISTINCT file_path FROM symbols
		WHERE file_path LIKE ? || '/%'
		ORDER BY file_path
	`, repoRoot)
	if err != nil {
		return files
	}
	defer rows.Close()

	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			continue
		}

		relPath := strings.TrimPrefix(filePath, repoRoot+"/")
		concern := categorizeByConcern(relPath)

		if files.ByConcern[concern] == nil {
			files.ByConcern[concern] = make(map[string]string)
		}

		// Get a brief description based on file name
		desc := describeFile(relPath)
		files.ByConcern[concern][relPath] = desc
	}

	return files
}

func categorizeByConcern(relPath string) string {
	parts := strings.Split(relPath, "/")
	if len(parts) == 0 {
		return "other"
	}

	// Check for internal packages
	if len(parts) >= 2 && parts[0] == pkgInternal {
		switch parts[1] {
		case "store":
			return "storage"
		case "query":
			return "query"
		case "index":
			return "indexing"
		case "output":
			return "output"
		case "config":
			return "configuration"
		case "search":
			return "search"
		case "embed":
			return "embeddings"
		case "context":
			return "context"
		}
	}

	// Top-level directories
	switch parts[0] {
	case pkgCmd:
		return "cli"
	case pkgInternal:
		return pkgInternal
	case "test":
		return "testing"
	}

	return "other"
}

func describeFile(relPath string) string {
	base := filepath.Base(relPath)
	name := strings.TrimSuffix(base, ".go")

	descriptions := map[string]string{
		"main":        "Application entry point",
		"root":        "CLI root command",
		"store":       "Database operations",
		"schema":      "Database schema",
		"types":       "Type definitions",
		"config":      "Configuration handling",
		"loader":      "Package loading",
		"refs":        "Reference extraction",
		"symbols":     "Symbol extraction",
		"callgraph":   "Call graph analysis",
		"lookup":      "Symbol lookup",
		"position":    "Position-based queries",
		"rg":          "Ripgrep integration",
		"json":        "JSON output formatting",
		"fingerprint": "Index fingerprinting",
		"generate":    "Code generation",
		"imports":     "Import analysis",
		"doctor":      "Health checks",
		"search":      "Search command",
		"def":         "Definition lookup",
		"show":        "Symbol display",
		"index":       "Index command",
		"callers":     "Caller analysis",
		"callees":     "Callee analysis",
		"version":     "Version information",
		"baseline":    "Performance baseline",
		"sim":         "Similarity search",
		"write":       "Write operations",
		"client":      "Client implementation",
		"vector":      "Vector operations",
		"state":       "State management",
		"history":     "History tracking",
		"metrics":     "Metrics collection",
		"degradation": "Graceful degradation",
	}

	if desc, ok := descriptions[name]; ok {
		return desc
	}
	return "Implementation"
}

func generateSymbols(db *sql.DB, repoRoot string, full bool, maxSymbols int) Symbols {
	syms := Symbols{}

	limit := maxSymbols
	if full {
		limit = 1000
	}

	// Get types (structs, interfaces, type aliases)
	typeRows, err := db.Query(`
		SELECT name, file_path, line_start
		FROM symbols
		WHERE kind IN ('type', 'interface', 'struct')
		  AND file_path LIKE ? || '/%'
		ORDER BY name
		LIMIT ?
	`, repoRoot, limit)
	if err == nil {
		defer typeRows.Close()
		for typeRows.Next() {
			var ref SymbolRef
			var fullPath string
			if err := typeRows.Scan(&ref.Name, &fullPath, &ref.Line); err != nil {
				continue
			}
			ref.File = strings.TrimPrefix(fullPath, repoRoot+"/")
			syms.Types = append(syms.Types, ref)
		}
	}

	// Get functions (prioritize exported ones)
	funcRows, err := db.Query(`
		SELECT name, file_path, line_start
		FROM symbols
		WHERE kind IN ('func', 'method')
		  AND file_path LIKE ? || '/%'
		  AND name GLOB '[A-Z]*'
		ORDER BY name
		LIMIT ?
	`, repoRoot, limit)
	if err == nil {
		defer funcRows.Close()
		for funcRows.Next() {
			var ref SymbolRef
			var fullPath string
			if err := funcRows.Scan(&ref.Name, &fullPath, &ref.Line); err != nil {
				continue
			}
			ref.File = strings.TrimPrefix(fullPath, repoRoot+"/")
			syms.Functions = append(syms.Functions, ref)
		}
	}

	return syms
}

func generateMeta(db *sql.DB) Meta {
	meta := Meta{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Get git commit
	if out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		meta.GitCommit = strings.TrimSpace(string(out))
	}

	// Get index fingerprint
	var fp sql.NullString
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'fingerprint'`).Scan(&fp); err == nil && fp.Valid {
		meta.IndexFingerprint = fp.String
	}

	return meta
}

func fileExists(path string) bool {
	_, err := exec.Command("test", "-f", path).Output()
	return err == nil
}
