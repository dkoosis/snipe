// Package context provides interface signal detection for symbols.
package context

import (
	"database/sql"
	"fmt"
	"strings"
)

// Interface-specific role constants for standard library implementations.
// These extend the Role constants in roles.go.
const (
	// RoleErrorType indicates a type that implements the error interface.
	RoleErrorType Role = "error_type"
	// RoleStringer indicates a type that implements fmt.Stringer.
	RoleStringer Role = "stringer"
	// RoleSortable indicates a type that implements sort.Interface.
	RoleSortable Role = "sortable"
)

// interfaceSignature maps a method name to its signature pattern and role.
type interfaceSignature struct {
	methodName string
	// sigPattern is a substring that must appear in the signature.
	// Empty string means any signature matches.
	sigPattern string
	role       Role
}

// knownInterfaces maps method signatures to their interface roles.
// Methods are grouped by the interface they implement.
var knownInterfaces = []interfaceSignature{
	// http.Handler: ServeHTTP(http.ResponseWriter, *http.Request)
	{methodName: "ServeHTTP", sigPattern: "ResponseWriter", role: RoleHTTPHandler},

	// io.Reader: Read([]byte) (int, error)
	{methodName: "Read", sigPattern: "[]byte", role: RoleIO},

	// io.Writer: Write([]byte) (int, error)
	{methodName: "Write", sigPattern: "[]byte", role: RoleIO},

	// io.Closer: Close() error
	{methodName: "Close", sigPattern: "error", role: RoleIO},

	// error: Error() string
	{methodName: "Error", sigPattern: "string", role: RoleErrorType},

	// fmt.Stringer: String() string
	{methodName: "String", sigPattern: "string", role: RoleStringer},
}

// sortInterfaceMethods lists the methods required for sort.Interface.
// A type must implement all three to be considered sortable.
var sortInterfaceMethods = []string{"Len", "Less", "Swap"}

// DetectInterfaceSignals scans the symbol database for methods that implement
// known standard library interfaces and returns a map of symbol IDs to role hints.
//
// The function detects:
//   - http.Handler (ServeHTTP method) -> handler
//   - io.Reader/Writer/Closer (Read/Write/Close methods) -> io_primitive
//   - error interface (Error method) -> error_type
//   - fmt.Stringer (String method) -> stringer
//   - sort.Interface (Len, Less, Swap methods) -> sortable
func DetectInterfaceSignals(db *sql.DB, repoRoot string) (map[string]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	signals := make(map[string]string)

	// Detect single-method interfaces
	if err := detectSingleMethodInterfaces(db, repoRoot, signals); err != nil {
		return nil, fmt.Errorf("detect single-method interfaces: %w", err)
	}

	// Detect sort.Interface (requires all three methods)
	if err := detectSortInterface(db, repoRoot, signals); err != nil {
		return nil, fmt.Errorf("detect sort.Interface: %w", err)
	}

	return signals, nil
}

// detectSingleMethodInterfaces finds methods matching known interface signatures.
func detectSingleMethodInterfaces(db *sql.DB, repoRoot string, signals map[string]string) error {
	for _, sig := range knownInterfaces {
		query := `
			SELECT id, name, signature, receiver
			FROM symbols
			WHERE kind = 'method'
			  AND name = ?
			  AND file_path LIKE ? || '/%'
		`

		rows, err := db.Query(query, sig.methodName, repoRoot)
		if err != nil {
			return fmt.Errorf("query methods for %s: %w", sig.methodName, err)
		}

		if err := processMethodRows(rows, sig, signals); err != nil {
			return err
		}
	}

	return nil
}

// processMethodRows processes query results and matches signatures.
func processMethodRows(rows *sql.Rows, sig interfaceSignature, signals map[string]string) error {
	defer rows.Close()

	for rows.Next() {
		var id, name string
		var signature, receiver sql.NullString

		if err := rows.Scan(&id, &name, &signature, &receiver); err != nil {
			return fmt.Errorf("scan method row: %w", err)
		}

		// Check if signature matches the pattern
		if matchesSignature(signature.String, sig) {
			signals[id] = string(sig.role)
		}
	}

	return rows.Err()
}

// matchesSignature checks if a method signature matches an interface pattern.
func matchesSignature(signature string, sig interfaceSignature) bool {
	// If no pattern specified, any signature matches
	if sig.sigPattern == "" {
		return true
	}

	// Check if signature contains the required pattern
	return strings.Contains(signature, sig.sigPattern)
}

// detectSortInterface finds types that implement sort.Interface.
// A type must have all three methods: Len() int, Less(i, j int) bool, Swap(i, j int).
func detectSortInterface(db *sql.DB, repoRoot string, signals map[string]string) error {
	// Query to find all receivers that have methods matching sort.Interface names
	query := `
		SELECT receiver, GROUP_CONCAT(name) as methods, GROUP_CONCAT(id) as ids
		FROM symbols
		WHERE kind = 'method'
		  AND name IN ('Len', 'Less', 'Swap')
		  AND receiver IS NOT NULL
		  AND receiver != ''
		  AND file_path LIKE ? || '/%'
		GROUP BY receiver
	`

	rows, err := db.Query(query, repoRoot)
	if err != nil {
		return fmt.Errorf("query sort.Interface methods: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var receiver, methodsConcat, idsConcat string
		if err := rows.Scan(&receiver, &methodsConcat, &idsConcat); err != nil {
			return fmt.Errorf("scan sort.Interface row: %w", err)
		}

		// Check if all three methods are present
		methods := strings.Split(methodsConcat, ",")
		if hasSortInterfaceMethods(methods) {
			// Mark all three method symbols as sortable
			ids := strings.Split(idsConcat, ",")
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if id != "" {
					signals[id] = string(RoleSortable)
				}
			}
		}
	}

	return rows.Err()
}

// hasSortInterfaceMethods checks if a slice of method names contains all sort.Interface methods.
func hasSortInterfaceMethods(methods []string) bool {
	found := make(map[string]bool)
	for _, m := range methods {
		m = strings.TrimSpace(m)
		found[m] = true
	}

	for _, required := range sortInterfaceMethods {
		if !found[required] {
			return false
		}
	}

	return true
}

// GetReceiverTypeID looks up the type symbol ID for a given receiver.
// This is useful for propagating role hints from methods to their types.
func GetReceiverTypeID(db *sql.DB, receiver string, repoRoot string) (string, error) {
	if receiver == "" {
		return "", nil
	}

	// Extract type name from receiver (e.g., "*MyType" -> "MyType", "MyType" -> "MyType")
	typeName := strings.TrimPrefix(receiver, "*")
	typeName = strings.TrimSpace(typeName)

	var id string
	err := db.QueryRow(`
		SELECT id
		FROM symbols
		WHERE name = ?
		  AND kind IN ('type', 'struct', 'interface')
		  AND file_path LIKE ? || '/%'
		LIMIT 1
	`, typeName, repoRoot).Scan(&id)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query receiver type: %w", err)
	}

	return id, nil
}
