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

// InferRoles analyzes symbols in the database and classifies them by architectural role.
// It queries the symbols, refs, call_graph, and imports tables to determine each symbol's role.
func InferRoles(db *sql.DB, repoRoot string) ([]SymbolRole, error) {
	var results []SymbolRole

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

	// Collect symbols for processing
	type symbolInfo struct {
		id        string
		name      string
		kind      string
		signature sql.NullString
		pkgPath   sql.NullString
		filePath  string
	}
	var symbols []symbolInfo

	for rows.Next() {
		var s symbolInfo
		if err := rows.Scan(&s.id, &s.name, &s.kind, &s.signature, &s.pkgPath, &s.filePath); err != nil {
			continue
		}
		symbols = append(symbols, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Process each symbol to determine its role
	for _, s := range symbols {
		sr := SymbolRole{
			SymbolID:   s.id,
			Name:       s.name,
			Visibility: inferVisibility(s.name),
		}
		if s.pkgPath.Valid {
			sr.PkgPath = s.pkgPath.String
		}

		// Determine role based on kind
		var role Role
		switch s.kind {
		case kindFunc, kindMethod:
			role = inferRole(db, s.id, s.name, s.kind, s.signature.String, s.pkgPath.String)
			// Risk classes are orthogonal to structural role and accumulate
			// independently (funcs/methods only — the heuristics are call/signature based).
			sr.RiskFlags = detectRiskFlags(db, s.id, s.signature.String, s.filePath)
		case kindStruct, kindInterface, kindType:
			role = inferRoleForType(db, s.id, s.name, s.pkgPath.String)
		default:
			role = RoleInternal
		}
		sr.Role = role

		results = append(results, sr)
	}

	return results, nil
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
// Recall caveat (sn-zd2 / follow-up sn-hmz): the `go func` / goroutine-spawn case
// is NOT detectable here because `go` statements and channel ops aren't emitted as
// refs, calls, or signature tokens. Concurrency recall is capped to channel-typed
// signatures and sync-primitive call sites until the indexer emits a `go` signal.
func detectRiskFlags(db *sql.DB, symbolID, signature, filePath string) []string {
	var flags []string
	if isConcurrency(db, symbolID, signature, filePath) {
		flags = append(flags, RiskConcurrency)
	}
	if isSecurityBoundary(db, symbolID, filePath) {
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

// isConcurrency reports whether a symbol handles concurrency:
//  1. its signature declares a channel type ("chan T", "chan<- T", "<-chan T"), or
//  2. its file imports sync/sync/atomic AND an enclosed call attributes to a sync
//     primitive (Lock/Unlock/Wait/Do/Add/Store/Load/...).
func isConcurrency(db *sql.DB, symbolID, signature, filePath string) bool {
	if hasChannelType(signature) {
		return true
	}
	if !fileImportsAny(db, filePath, "sync", "sync/atomic") {
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
func isSecurityBoundary(db *sql.DB, symbolID, filePath string) bool {
	if fileImportsAny(db, filePath, "os/exec") && enclosedCallCtx(db, symbolID, execCallCtxs) {
		return true
	}
	if fileImportsAny(db, filePath, "crypto/tls") && enclosedRefMatches(db, symbolID, "InsecureSkipVerify") {
		return true
	}
	if fileImportsAny(db, filePath, "net/http") && enclosedCallCtx(db, symbolID, serverCallCtxs) {
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

// fileImportsAny reports whether filePath imports any of the given package paths.
func fileImportsAny(db *sql.DB, filePath string, pkgs ...string) bool {
	if filePath == "" || len(pkgs) == 0 {
		return false
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(pkgs)), ",")
	args := make([]any, 0, len(pkgs)+1)
	args = append(args, filePath)
	for _, p := range pkgs {
		args = append(args, p)
	}
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM imports WHERE file_path = ? AND pkg_path IN (`+placeholders+`)`,
		args...,
	).Scan(&n)
	return err == nil && n > 0
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
