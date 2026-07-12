// Package context provides role inference for symbols.
package context

import (
	"database/sql"
	"strings"
	"unicode"
)

// Role represents an architectural role classification for a symbol.
// This is distinct from RoleHint which focuses on interface implementations.
type Role string

// Role constants define the architectural classification categories.
const (
	// RoleEntryPoint indicates a symbol that is called by main/init, is main, or is cobra.Command.RunE.
	RoleEntryPoint Role = "entry_point"
	// RoleAPIBoundary indicates an exported symbol that has callers from other packages.
	RoleAPIBoundary Role = "api_boundary"
	// RolePersistence indicates a symbol in a store package or that calls sql.DB methods.
	RolePersistence Role = "persistence"
	// RoleHTTPHandler indicates a symbol that implements http.Handler or matches http handler signature.
	RoleHTTPHandler Role = "handler"
	// RoleIO indicates a symbol that implements io.Reader, io.Writer, or io.Closer.
	RoleIO Role = "io_primitive"
	// RoleFactory indicates a symbol whose name starts with "New" and returns a pointer.
	RoleFactory Role = "factory"
	// RoleInternal indicates an unexported symbol or one only called within its package.
	RoleInternal Role = "internal"
)

// Risk classes are orthogonal to Role (structural). A symbol may carry zero or
// more risk flags in addition to its single structural Role (sn-zd2, decision A).
const (
	// RiskConcurrency flags a symbol that touches goroutine/channel/sync
	// machinery — a channel-typed signature or a sync-primitive call site.
	RiskConcurrency = "concurrency"
	// RiskSecurityBoundary flags a symbol that crosses a security boundary —
	// spawns external processes (os/exec), disables TLS verification, or stands
	// up a network server.
	RiskSecurityBoundary = "security_boundary"
)

// Symbol kind constants used for role inference.
const (
	kindFunc      = "func"
	kindMethod    = "method"
	kindStruct    = "struct"
	kindInterface = "interface"
	kindType      = "type"
)

// Entry point name constants (shared across role inference and flow analysis).
const (
	epMain    = "main"
	epExecute = "Execute"
)

// Visibility represents the export status of a symbol.
type Visibility string

// Visibility constants.
const (
	// VisibilityExported indicates a symbol that starts with uppercase (exported).
	VisibilityExported Visibility = "exported"
	// VisibilityPackagePrivate indicates a symbol that starts with lowercase (unexported).
	VisibilityPackagePrivate Visibility = "package_private"
)

// SymbolRole contains the inferred role and visibility for a symbol.
type SymbolRole struct {
	SymbolID   string     `json:"symbol_id"`
	Name       string     `json:"name"`
	Role       Role       `json:"role"`
	RiskFlags  []string   `json:"risk_flags,omitempty"`
	Visibility Visibility `json:"visibility"`
	PkgPath    string     `json:"pkg_path,omitempty"`
}

// symbolInfo is the raw metadata a symbol needs for role classification.
type symbolInfo struct {
	id        string
	name      string
	kind      string
	signature sql.NullString
	pkgPath   sql.NullString
	filePath  string
}

// classifySymbol applies the structural role + orthogonal risk-flag heuristics to
// one symbol. Shared by the whole-repo InferRoles and the targeted RolesForSymbols
// so both classify identically.
func classifySymbol(db *sql.DB, imports importsCache, s symbolInfo) SymbolRole {
	sr := SymbolRole{
		SymbolID:   s.id,
		Name:       s.name,
		Visibility: inferVisibility(s.name),
	}
	if s.pkgPath.Valid {
		sr.PkgPath = s.pkgPath.String
	}
	switch s.kind {
	case kindFunc, kindMethod:
		sr.Role = inferRole(db, s.id, s.name, s.kind, s.signature.String, s.pkgPath.String)
		// Risk classes are orthogonal to structural role and accumulate
		// independently (funcs/methods only — the heuristics are call/signature based).
		sr.RiskFlags = detectRiskFlags(db, imports, s.id, s.signature.String, s.filePath)
	case kindStruct, kindInterface, kindType:
		sr.Role = inferRoleForType(db, s.id, s.name, s.pkgPath.String)
	default:
		sr.Role = RoleInternal
	}
	return sr
}

// scanSymbolInfos collects symbolInfo rows from a query over the symbols table.
func scanSymbolInfos(rows *sql.Rows) ([]symbolInfo, error) {
	var symbols []symbolInfo
	for rows.Next() {
		var s symbolInfo
		if err := rows.Scan(&s.id, &s.name, &s.kind, &s.signature, &s.pkgPath, &s.filePath); err != nil {
			continue
		}
		symbols = append(symbols, s)
	}
	return symbols, rows.Err()
}

// InferRoles analyzes symbols in the database and classifies them by architectural role.
// It queries the symbols, refs, call_graph, and imports tables to determine each symbol's role.
func InferRoles(db *sql.DB, repoRoot string) ([]SymbolRole, error) {
	// Query all functions, methods, and types with their metadata
	rows, err := db.Query(`
		SELECT id, name, kind, signature, pkg_path, file_path
		FROM symbols
		WHERE kind IN ('func', 'method', 'struct', 'interface', 'type')
		  AND file_path LIKE ? || '/%'
		  AND file_path NOT LIKE '%/example%'
		  AND file_path NOT LIKE '%/testdata%'
		  AND file_path NOT LIKE '%\_test.go' ESCAPE '\'
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	symbols, err := scanSymbolInfos(rows)
	if err != nil {
		return nil, err
	}

	// Preload the risk-relevant imports once so risk classification is a map
	// lookup per symbol, not a DB query per (symbol × package) probe (sn-c8o).
	imports, err := loadImportsCache(db, repoRoot)
	if err != nil {
		return nil, err
	}

	results := make([]SymbolRole, 0, len(symbols))
	for _, s := range symbols {
		results = append(results, classifySymbol(db, imports, s))
	}
	return results, nil
}

// RolesForSymbols classifies a specific set of symbol IDs — the fast path for
// callers (e.g. `snipe risk`) that already know which symbols changed and don't
// want a whole-repo InferRoles pass. Returns a map keyed by symbol ID; IDs that
// don't resolve to a func/method/type symbol are simply absent.
func RolesForSymbols(db *sql.DB, repoRoot string, ids []string) (map[string]SymbolRole, error) {
	if len(ids) == 0 {
		return map[string]SymbolRole{}, nil
	}
	imports, err := loadImportsCache(db, repoRoot)
	if err != nil {
		return nil, err
	}

	out := make(map[string]SymbolRole, len(ids))
	// Chunk the IN clause under SQLite's default host-parameter limit (999) — a
	// large diff can touch more symbols than a single query can bind.
	const batchSize = 900
	for start := 0; start < len(ids); start += batchSize {
		batch := ids[start:min(start+batchSize, len(ids))]

		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, id := range batch {
			args[i] = id
		}
		rows, err := db.Query(`
			SELECT id, name, kind, signature, pkg_path, file_path
			FROM symbols
			WHERE id IN (`+placeholders+`)
			  AND kind IN ('func', 'method', 'struct', 'interface', 'type')
		`, args...)
		if err != nil {
			return nil, err
		}
		symbols, err := scanSymbolInfos(rows)
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		for _, s := range symbols {
			out[s.id] = classifySymbol(db, imports, s)
		}
	}
	return out, nil
}

// inferVisibility determines if a symbol is exported based on its name.
func inferVisibility(name string) Visibility {
	if len(name) > 0 && unicode.IsUpper(rune(name[0])) {
		return VisibilityExported
	}
	return VisibilityPackagePrivate
}

// inferRole determines the architectural role of a symbol.
func inferRole(db *sql.DB, symbolID, name, kind, signature, pkgPath string) Role {
	// Check entry_point: is main, or is cobra.Command.RunE, or called by main/init
	if isEntryPoint(db, symbolID, name, kind, pkgPath) {
		return RoleEntryPoint
	}

	// Check handler: implements http.Handler or matches http handler signature
	if isHandler(signature) {
		return RoleHTTPHandler
	}

	// Check io_primitive: implements io.Reader, io.Writer, io.Closer
	if isIOPrimitive(signature) {
		return RoleIO
	}

	// Check factory: name starts with New and returns pointer
	if isFactory(name, signature) {
		return RoleFactory
	}

	// Check persistence: pkg_path contains "store" or calls sql.DB methods
	if isPersistence(db, symbolID, pkgPath) {
		return RolePersistence
	}

	// Check api_boundary: exported AND has callers from other packages
	if isAPIBoundary(db, symbolID, name, pkgPath) {
		return RoleAPIBoundary
	}

	// Check internal: unexported or only called within package
	if isInternal(db, symbolID, name, pkgPath) {
		return RoleInternal
	}

	// Default to internal for unexported, api_boundary for exported
	if inferVisibility(name) == VisibilityPackagePrivate {
		return RoleInternal
	}
	return RoleAPIBoundary
}

// isEntryPoint checks if a symbol is an entry point:
// - is named "main" in a main package
// - is a cobra.Command.RunE method
// - is called by main or init functions
func isEntryPoint(db *sql.DB, symbolID, name, kind, pkgPath string) bool {
	// Check if this is main function
	if name == epMain && strings.HasSuffix(pkgPath, "/main") {
		return true
	}

	// Check if this is a RunE method (cobra command)
	if name == "RunE" && kind == kindMethod {
		return true
	}

	// Check if this is an init function
	if name == "init" {
		return true
	}

	// Check if called by main or init functions
	var callerCount int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		WHERE cg.callee_id = ?
		  AND caller.name IN ('main', 'init')
	`, symbolID).Scan(&callerCount)
	if err == nil && callerCount > 0 {
		return true
	}

	// Check for cobra Execute patterns (often called from main)
	if name == epExecute && kind == kindMethod {
		return true
	}

	return false
}

// isHandler checks if a symbol implements http.Handler or matches http handler signature.
// Common patterns:
// - ServeHTTP(http.ResponseWriter, *http.Request)
// - func(http.ResponseWriter, *http.Request)
func isHandler(signature string) bool {
	if signature == "" {
		return false
	}
	sig := strings.ToLower(signature)

	// Check for ServeHTTP method signature
	if strings.Contains(sig, "servehttp") {
		return true
	}

	// Check for http handler function signature pattern
	if strings.Contains(sig, "responsewriter") && strings.Contains(sig, "request") {
		return true
	}

	// Check for gin/echo style handlers
	if strings.Contains(sig, "*gin.context") || strings.Contains(sig, "echo.context") {
		return true
	}

	return false
}

// isIOPrimitive checks if a symbol implements io.Reader, io.Writer, or io.Closer.
// These are identified by method signatures: Read([]byte), Write([]byte), Close()
func isIOPrimitive(signature string) bool {
	if signature == "" {
		return false
	}
	sig := strings.ToLower(signature)

	// Check for Reader pattern: Read(p []byte) (n int, err error)
	if strings.Contains(sig, "read(") && strings.Contains(sig, "[]byte") && strings.Contains(sig, "int") {
		return true
	}

	// Check for Writer pattern: Write(p []byte) (n int, err error)
	if strings.Contains(sig, "write(") && strings.Contains(sig, "[]byte") && strings.Contains(sig, "int") {
		return true
	}

	// Check for Closer pattern: Close() error
	if strings.Contains(sig, "close()") && strings.Contains(sig, "error") {
		return true
	}

	return false
}

// isFactory checks if a symbol is a factory function:
// - name starts with "New"
// - returns a pointer (signature contains "*")
func isFactory(name, signature string) bool {
	if !strings.HasPrefix(name, "New") {
		return false
	}

	// Must return something (have a return type)
	if signature == "" {
		return false
	}

	// Check for pointer return type (contains "*" after the closing paren of params)
	// Example: "func NewStore(path string) (*Store, error)"
	parenIdx := strings.LastIndex(signature, ")")
	if parenIdx == -1 || parenIdx >= len(signature)-1 {
		return false
	}

	returnPart := signature[parenIdx+1:]
	return strings.Contains(returnPart, "*")
}

// isPersistence checks if a symbol is related to persistence:
// - package path contains "store"
// - calls sql.DB methods (sql.Open, db.Query, db.Exec, etc.)
func isPersistence(db *sql.DB, symbolID, pkgPath string) bool {
	// Check if package path contains "store"
	if strings.Contains(strings.ToLower(pkgPath), "store") {
		return true
	}

	// Check if this symbol calls any sql-related symbols
	var sqlCallCount int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM call_graph cg
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE cg.caller_id = ?
		  AND (
		      callee.pkg_path LIKE '%database/sql%'
		      OR callee.name IN ('Query', 'QueryRow', 'Exec', 'Begin', 'Commit', 'Rollback', 'Prepare')
		  )
	`, symbolID).Scan(&sqlCallCount)
	if err == nil && sqlCallCount > 0 {
		return true
	}

	return false
}

// isAPIBoundary checks if a symbol is an API boundary:
// - is exported (name starts with uppercase)
// - has callers from other packages
func isAPIBoundary(db *sql.DB, symbolID, name, pkgPath string) bool {
	// Must be exported
	if inferVisibility(name) != VisibilityExported {
		return false
	}

	// Check for callers from other packages
	var crossPkgCallerCount int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		WHERE cg.callee_id = ?
		  AND caller.pkg_path IS NOT NULL
		  AND caller.pkg_path != ?
	`, symbolID, pkgPath).Scan(&crossPkgCallerCount)
	if err == nil && crossPkgCallerCount > 0 {
		return true
	}

	return false
}

// isInternal checks if a symbol is internal:
// - is unexported (name starts with lowercase)
// - OR only has callers from the same package
func isInternal(db *sql.DB, symbolID, name, pkgPath string) bool {
	// Unexported symbols are always internal
	if inferVisibility(name) == VisibilityPackagePrivate {
		return true
	}

	// Check if all callers are from the same package
	var totalCallers, samePkgCallers int

	// Get total caller count
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM call_graph
		WHERE callee_id = ?
	`, symbolID).Scan(&totalCallers)
	if err != nil {
		return false
	}

	// If no callers, could be internal or unused
	if totalCallers == 0 {
		return true
	}

	// Get same-package caller count
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		WHERE cg.callee_id = ?
		  AND caller.pkg_path = ?
	`, symbolID, pkgPath).Scan(&samePkgCallers)
	if err != nil {
		return false
	}

	// Internal if all callers are from same package
	return samePkgCallers == totalCallers
}

// inferRoleForType determines the architectural role of a type symbol.
// Types are classified by their package context and usage patterns.
func inferRoleForType(_ *sql.DB, _, name, pkgPath string) Role {
	// Persistence: type in a store-like package
	if strings.Contains(strings.ToLower(pkgPath), "store") {
		return RolePersistence
	}

	// IO: type names suggesting I/O (Reader, Writer, etc.)
	nameLower := strings.ToLower(name)
	if strings.Contains(nameLower, "reader") || strings.Contains(nameLower, "writer") {
		return RoleIO
	}

	// Default: exported types are API boundaries, unexported are internal
	if inferVisibility(name) == VisibilityExported {
		return RoleAPIBoundary
	}
	return RoleInternal
}

// detectRiskFlags returns the risk classes a func/method carries, orthogonal to
// its structural Role. Detection keys off signals that already exist in the index
// (signature, imports table, refs.ast_ctx call attribution) — no indexer change.
//
// Recall note (sn-zd2, closed by sn-hmz): the indexer now emits a synthetic
// self-attributed ref (ast_ctx="go"/"chan") for go-statements and channel
// sends/receives, attributed to the enclosing named function. isConcurrency
// checks for it ungated below — no import co-signal needed, since that
// ast_ctx is only ever emitted for a real GoStmt/channel op, unlike the
// name-based `call:Lock` attribution which needs the sync-import gate to
// avoid misfiring on a coincidentally-named method.
func detectRiskFlags(db *sql.DB, imports importsCache, symbolID, signature, filePath string) []string {
	var flags []string
	if isConcurrency(db, imports, symbolID, signature, filePath) {
		flags = append(flags, RiskConcurrency)
	}
	if isSecurityBoundary(db, imports, symbolID, filePath) {
		flags = append(flags, RiskSecurityBoundary)
	}
	return flags
}

// concurrencyCalls are sync/atomic primitive call names that, co-signalled with a
// sync or sync/atomic import in the same file, indicate real concurrency handling.
var concurrencyCallCtxs = []string{
	"call:Lock", "call:Unlock", "call:RLock", "call:RUnlock",
	"call:Wait", "call:Do", "call:Add", "call:Done",
	"call:Store", "call:Load",
}

// goChanCtxs are the ast_ctx values the indexer emits for a real go-statement
// or channel send/receive (self-attributed to the enclosing named func) —
// mirrors index.CtxGo/CtxChan as literals; this package doesn't import
// internal/index (existing convention here: concurrencyCallCtxs etc. are
// literal "call:" strings too, not index.CtxCallPrefix-derived).
// Unlike concurrencyCallCtxs, no import co-signal is needed: these values are
// only ever emitted for an actual GoStmt/SendStmt/receive, never name-derived.
var goChanCtxs = []string{"go", "chan"}

// isConcurrency reports whether a symbol handles concurrency:
//  1. its signature declares a channel type ("chan T", "chan<- T", "<-chan T"), or
//  2. it encloses a go-statement or channel send/receive (ast_ctx="go"/"chan"), or
//  3. its file imports sync/sync/atomic AND an enclosed call attributes to a sync
//     primitive (Lock/Unlock/Wait/Do/Add/Store/Load/...).
func isConcurrency(db *sql.DB, imports importsCache, symbolID, signature, filePath string) bool {
	if hasChannelType(signature) {
		return true
	}
	if enclosedCallCtx(db, symbolID, goChanCtxs) {
		return true
	}
	if !imports.fileImportsAny(filePath, "sync", "sync/atomic") {
		return false
	}
	return enclosedCallCtx(db, symbolID, concurrencyCallCtxs)
}

// execCalls are os/exec entry points that spawn external processes.
var execCallCtxs = []string{
	"call:Command", "call:CommandContext",
	"call:Run", "call:Start", "call:Output", "call:CombinedOutput",
}

// serverCalls stand up a network server (server-side net/http).
var serverCallCtxs = []string{
	"call:ListenAndServe", "call:ListenAndServeTLS", "call:Serve",
}

// isSecurityBoundary reports whether a symbol crosses a security boundary:
//  1. file imports os/exec AND an enclosed call spawns a process, or
//  2. file imports crypto/tls AND the symbol references InsecureSkipVerify, or
//  3. file imports net/http AND the symbol stands up a server.
//
// Each rule requires the risky import as a co-signal so a name-only call
// attribution (e.g. a method literally named Command) can't misfire alone.
func isSecurityBoundary(db *sql.DB, imports importsCache, symbolID, filePath string) bool {
	if imports.fileImportsAny(filePath, "os/exec") && enclosedCallCtx(db, symbolID, execCallCtxs) {
		return true
	}
	if imports.fileImportsAny(filePath, "crypto/tls") && enclosedRefMatches(db, symbolID, "InsecureSkipVerify") {
		return true
	}
	if imports.fileImportsAny(filePath, "net/http") && enclosedCallCtx(db, symbolID, serverCallCtxs) {
		return true
	}
	return false
}

// hasChannelType reports whether a signature declares a channel-typed parameter
// or result. It matches the `chan` keyword only at a token boundary so names like
// "Exchange" (which contains "chan") don't misfire.
func hasChannelType(signature string) bool {
	const kw = "chan"
	for i := 0; i+len(kw) <= len(signature); i++ {
		if signature[i:i+len(kw)] != kw {
			continue
		}
		if i > 0 && isIdentByte(signature[i-1]) {
			continue // preceded by ident char → part of a larger word
		}
		if j := i + len(kw); j < len(signature) && isIdentByte(signature[j]) {
			continue // followed by ident char → part of a larger word
		}
		return true
	}
	return false
}

// isIdentByte reports whether b can appear inside a Go identifier.
func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// riskImportPkgs are the only import paths risk classification cares about. The
// importsCache preloads exactly these so a whole InferRoles pass costs one imports
// query, not one per (symbol × package) probe.
var riskImportPkgs = []string{"sync", "sync/atomic", "os/exec", "crypto/tls", "net/http"}

// importsCache maps a file path to the set of risk-relevant packages it imports.
// It's built once per InferRoles run (loadImportsCache) and threaded through the
// risk heuristics, replacing the per-symbol fileImportsAny query (sn-c8o).
type importsCache map[string]map[string]bool

// loadImportsCache preloads, in a single query, which files under repoRoot import
// any riskImportPkgs package. Files importing none simply don't appear.
func loadImportsCache(db *sql.DB, repoRoot string) (importsCache, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(riskImportPkgs)), ",")
	args := make([]any, 0, len(riskImportPkgs)+1)
	for _, p := range riskImportPkgs {
		args = append(args, p)
	}
	args = append(args, repoRoot)
	rows, err := db.Query(
		`SELECT file_path, pkg_path FROM imports
		 WHERE pkg_path IN (`+placeholders+`) AND file_path LIKE ? || '/%'`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cache := make(importsCache)
	for rows.Next() {
		var fp, pp string
		if err := rows.Scan(&fp, &pp); err != nil {
			continue
		}
		set := cache[fp]
		if set == nil {
			set = make(map[string]bool)
			cache[fp] = set
		}
		set[pp] = true
	}
	return cache, rows.Err()
}

// fileImportsAny reports whether filePath imports any of the given package paths.
func (c importsCache) fileImportsAny(filePath string, pkgs ...string) bool {
	if filePath == "" || len(pkgs) == 0 {
		return false
	}
	set := c[filePath]
	if set == nil {
		return false
	}
	for _, p := range pkgs {
		if set[p] {
			return true
		}
	}
	return false
}

// enclosedCallCtx reports whether the symbol encloses a ref whose ast_ctx matches
// any of the given call attributions. Depends on refs.enclosing_id + ast_ctx
// (schema v18+); a pre-v18 index yields NULL ctx → no signal (mirrors the
// lifecycle R1/R2 trap).
func enclosedCallCtx(db *sql.DB, symbolID string, ctxs []string) bool {
	if len(ctxs) == 0 {
		return false
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ctxs)), ",")
	args := make([]any, 0, len(ctxs)+1)
	args = append(args, symbolID)
	for _, c := range ctxs {
		args = append(args, c)
	}
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM refs WHERE enclosing_id = ? AND ast_ctx IN (`+placeholders+`)`,
		args...,
	).Scan(&n)
	return err == nil && n > 0
}

// enclosedRefMatches reports whether the symbol encloses a ref whose ast_ctx or
// snippet contains needle (used for field-access signals like InsecureSkipVerify
// that aren't call attributions).
func enclosedRefMatches(db *sql.DB, symbolID, needle string) bool {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM refs
		 WHERE enclosing_id = ?
		   AND (ast_ctx LIKE '%' || ? || '%' OR snippet LIKE '%' || ? || '%')`,
		symbolID, needle, needle,
	).Scan(&n)
	return err == nil && n > 0
}

// InferRoleForSymbol returns the role for a single symbol without scanning the entire DB.
// The filePath parameter is accepted for call-site convenience but unused.
func InferRoleForSymbol(db *sql.DB, symbolID, name, kind, signature, pkgPath, _ /* filePath */ string) Role {
	if kind == kindFunc || kind == kindMethod {
		return inferRole(db, symbolID, name, kind, signature, pkgPath)
	}
	// Types: use inferRoleForType for meaningful classification
	if kind == kindStruct || kind == kindInterface || kind == kindType {
		return inferRoleForType(db, symbolID, name, pkgPath)
	}
	if inferVisibility(name) == VisibilityPackagePrivate {
		return RoleInternal
	}
	return RoleAPIBoundary
}
