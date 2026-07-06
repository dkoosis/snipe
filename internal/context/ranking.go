// Package context provides role-weighted symbol ranking for boot context generation.
package context

import (
	"database/sql"
	"math"
	"sort"
	"strings"
)

// roleWeights defines priority multipliers for each architectural role.
// Higher weights indicate more architecturally significant symbols.
var roleWeights = map[Role]float64{
	RoleEntryPoint:  10,
	RoleAPIBoundary: 5,
	RolePersistence: 5,
	RoleHTTPHandler: 5,
	RoleFactory:     3,
	RoleIO:          3,
	RoleInternal:    1,
}

// riskWeights boost symbols carrying a risk flag so architecturally significant
// concurrency/security code surfaces in ranked boot output even when its
// structural role is weak (e.g. an unexported goroutine helper). Risk is
// orthogonal to Role (sn-zd2 decision A): the effective weight is the max of the
// structural role weight and any risk weight, not a product.
var riskWeights = map[string]float64{
	RiskSecurityBoundary: 6,
	RiskConcurrency:      5,
}

// RankedSymbol represents a symbol with its calculated priority score.
type RankedSymbol struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	RefCount  int      `json:"ref_count"`
	Role      string   `json:"role"`
	RiskFlags []string `json:"risk_flags,omitempty"`
	Priority  float64  `json:"priority"`
	Doc       string   `json:"doc,omitempty"`
}

// CalculatePriority computes the priority score for a symbol from its structural
// role alone. Formula: priority = (log(ref_count) + 1) * role_weight.
func CalculatePriority(refCount int, role Role) float64 {
	return calcPriority(refCount, effectiveWeight(role, nil))
}

// calcPriority balances reference count with a supplied architectural weight.
// Uses log(ref_count + 1) to handle zero refs and dampen high ref counts;
// adding 1 ensures log is never negative for ref_count >= 0.
func calcPriority(refCount int, weight float64) float64 {
	logRefs := math.Log(float64(refCount) + 1)
	return (logRefs + 1) * weight
}

// effectiveWeight returns the max of a symbol's structural-role weight and any
// risk-flag weight. Risk is orthogonal to role (decision A): a security_boundary
// internal helper is boosted to the security weight without compounding.
func effectiveWeight(role Role, riskFlags []string) float64 {
	weight, ok := roleWeights[role]
	if !ok {
		weight = 1 // Default to internal weight
	}
	for _, f := range riskFlags {
		if rw := riskWeights[f]; rw > weight {
			weight = rw
		}
	}
	return weight
}

// RankSymbols queries symbols from the database, infers their roles,
// calculates priority scores, and returns them sorted by priority.
// Performance optimizations:
// - Single batch query for symbols and ref counts (no loop queries)
// - In-memory role lookup using map built from InferRoles
// - Early limiting via SQL ORDER BY + LIMIT after scoring
func RankSymbols(db *sql.DB, repoRoot string, limit int) ([]RankedSymbol, error) {
	// Step 1: Infer roles for all symbols in one batch
	// This builds the role map upfront to avoid per-symbol queries
	symbolRoles, err := InferRoles(db, repoRoot)
	if err != nil {
		return nil, err
	}

	// Build role + risk lookup maps by symbol ID for O(1) access
	roleMap := make(map[string]Role, len(symbolRoles))
	riskMap := make(map[string][]string, len(symbolRoles))
	for _, sr := range symbolRoles {
		roleMap[sr.SymbolID] = sr.Role
		if len(sr.RiskFlags) > 0 {
			riskMap[sr.SymbolID] = sr.RiskFlags
		}
	}

	// Step 2: Query all symbols with their ref counts in a single batch query
	// This avoids N+1 query problem by using a LEFT JOIN with COUNT
	rows, err := db.Query(`
		SELECT
			s.id,
			s.name,
			s.file_path,
			s.line_start,
			COUNT(r.id) as ref_count,
			COALESCE(s.doc, '') as doc
		FROM symbols s
		LEFT JOIN refs r ON r.symbol_id = s.id
		WHERE s.kind IN ('func', 'method', 'type', 'interface', 'struct')
		  AND s.file_path LIKE ? || '/%'
		  AND s.file_path NOT LIKE '%/example%'
		  AND s.file_path NOT LIKE '%/testdata%'
		  AND s.file_path NOT LIKE '%/testutil/%'
		  AND s.file_path NOT LIKE '%test/%'
		  AND s.file_path NOT LIKE '%\_test.go' ESCAPE '\'
		GROUP BY s.id
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Step 3: Calculate priorities and collect results
	var results []RankedSymbol
	for rows.Next() {
		var rs RankedSymbol
		if err := rows.Scan(&rs.ID, &rs.Name, &rs.File, &rs.Line, &rs.RefCount, &rs.Doc); err != nil {
			continue // Skip malformed rows
		}

		// Look up role from pre-built map (O(1))
		role, ok := roleMap[rs.ID]
		if !ok {
			// Default based on name casing if not in role map
			role = RoleInternal
			if len(rs.Name) > 0 && rs.Name[0] >= 'A' && rs.Name[0] <= 'Z' {
				role = RoleAPIBoundary
			}
		}
		rs.Role = string(role)
		rs.RiskFlags = riskMap[rs.ID]
		rs.Priority = calcPriority(rs.RefCount, effectiveWeight(role, rs.RiskFlags))

		results = append(results, rs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Step 4: Sort by priority descending
	sort.Slice(results, func(i, j int) bool {
		// Primary sort by priority (descending)
		if results[i].Priority != results[j].Priority {
			return results[i].Priority > results[j].Priority
		}
		// Secondary sort by ref count (descending) for equal priorities
		if results[i].RefCount != results[j].RefCount {
			return results[i].RefCount > results[j].RefCount
		}
		// Tertiary sort by name (ascending) for stability
		if results[i].Name != results[j].Name {
			return results[i].Name < results[j].Name
		}
		// Quaternary sort by file path (ascending) — disambiguates same-named
		// symbols across packages (e.g. alpha.Ambiguous vs beta.Ambiguous).
		return results[i].File < results[j].File
	})

	// Step 5: Enforce per-package diversity — no single package dominates key symbols.
	// Cap at 3 symbols per package directory to ensure architectural breadth.
	// Risk-flagged symbols (concurrency/security_boundary) are exempt: they're
	// architecturally significant and the cap would otherwise starve them to 0 on
	// large repos even when detected (sn-zd2 step 5).
	const maxPerPackage = 3
	pkgCount := make(map[string]int)
	diverse := make([]RankedSymbol, 0, len(results))
	for i := range results {
		rs := &results[i]
		if len(rs.RiskFlags) == 0 {
			pkg := packageDir(rs.File, repoRoot)
			if pkgCount[pkg] >= maxPerPackage {
				continue
			}
			pkgCount[pkg]++
		}
		diverse = append(diverse, *rs)
	}
	results = diverse

	// Step 6: Apply limit
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// packageDir extracts the package directory from a file path for diversity grouping.
func packageDir(filePath, repoRoot string) string {
	rel := strings.TrimPrefix(filePath, repoRoot+"/")
	if idx := strings.LastIndex(rel, "/"); idx >= 0 {
		return rel[:idx]
	}
	return rel
}
