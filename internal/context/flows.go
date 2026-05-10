package context

import (
	"bufio"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// flowNode represents a symbol in the call flow graph.
type flowNode struct {
	id       string
	name     string
	receiver string
}

// ExtractPrimaryFlows traces call paths from entry points and returns flow strings.
// It finds main, Execute, RunE functions and traces their call paths.
// Returns flow strings like "main -> Execute -> Store.Open -> WriteIndex"
// Performance: Uses batch queries - no queries in loops.
func ExtractPrimaryFlows(db *sql.DB, repoRoot string, maxDepth int) ([]string, error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}

	// Query 1: Get all entry points (main, Execute, RunE, init)
	entryPoints, err := queryEntryPointSymbols(db, repoRoot)
	if err != nil {
		return nil, err
	}

	if len(entryPoints) == 0 {
		return nil, nil
	}

	// Query 2: Pre-fetch call graph data for all symbols up to maxDepth
	// We batch-load the entire relevant portion of the call graph
	callGraph, err := queryCallGraphBatch(db, repoRoot)
	if err != nil {
		return nil, err
	}

	// Query 3: Pre-fetch symbol names for all symbols in the call graph
	symbolNames, err := querySymbolNamesBatch(db, repoRoot)
	if err != nil {
		return nil, err
	}

	// Build all candidate flows, then select diverse set
	type candidateFlow struct {
		name     string
		flow     string
		priority int // lower = higher priority
	}

	var candidates []candidateFlow
	for _, ep := range entryPoints {
		flow := buildFlowPath(ep.id, ep.name, ep.receiver, callGraph, symbolNames, maxDepth)
		if flow == "" {
			continue
		}
		pri := flowPriority(ep.name)
		candidates = append(candidates, candidateFlow{name: ep.name, flow: flow, priority: pri})
	}

	// Sort by priority (core commands first), then alphabetically
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].name < candidates[j].name
	})

	// Select diverse flows — ensure core commands get representation while
	// avoiding redundant traces of the same call chain.
	maxFlows := 7
	var flows []string
	seenTerminal := make(map[string]bool) // terminal path dedup for non-core
	seenSuffix := make(map[string]bool)   // full suffix dedup for core commands
	for _, c := range candidates {
		if len(flows) >= maxFlows {
			break
		}
		parts := strings.Split(c.flow, " -> ")
		terminal := ""
		if len(parts) >= 2 {
			terminal = parts[len(parts)-2] + " -> " + parts[len(parts)-1]
		}
		// For core commands: dedup on the call chain (everything after entry point)
		// so runDef and runCallees don't both show "LookupByName → lookupMethod → scanSymbolRows"
		if c.priority <= 2 && len(parts) >= 2 {
			suffix := strings.Join(parts[1:], " -> ")
			if seenSuffix[suffix] {
				continue
			}
			seenSuffix[suffix] = true
		} else if seenTerminal[terminal] {
			continue
		}
		seenTerminal[terminal] = true
		flows = append(flows, c.flow)
	}

	return flows, nil
}

// queryEntryPointSymbols returns symbols that are entry points.
// Includes main, Execute, and cobra RunE handlers (runX functions in cmd/).
func queryEntryPointSymbols(db *sql.DB, repoRoot string) ([]flowNode, error) {
	rows, err := db.Query(`
		SELECT id, name, COALESCE(receiver, '') as receiver
		FROM symbols
		WHERE file_path LIKE ? || '/%'
		  AND file_path NOT LIKE '%\_test.go' ESCAPE '\'
		  AND kind IN ('func', 'method')
		  AND (
		      name = 'main'
		      OR name = 'Execute'
		      OR (name LIKE 'run%' AND file_path LIKE '%/cmd/%' AND name GLOB 'run[A-Z]*')
		  )
		ORDER BY
		  CASE
		    WHEN name = 'main' THEN 1
		    WHEN name = 'Execute' THEN 2
		    ELSE 3
		  END,
		  name
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []flowNode
	for rows.Next() {
		var n flowNode
		if err := rows.Scan(&n.id, &n.name, &n.receiver); err != nil {
			continue
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// queryCallGraphBatch fetches the entire call graph for the repo in one query.
// Returns map[caller_id][]callee_id
func queryCallGraphBatch(db *sql.DB, repoRoot string) (map[string][]string, error) {
	rows, err := db.Query(`
		SELECT cg.caller_id, cg.callee_id
		FROM call_graph cg
		JOIN symbols caller ON cg.caller_id = caller.id
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE caller.file_path LIKE ? || '/%'
		  AND callee.file_path LIKE ? || '/%'
	`, repoRoot, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	graph := make(map[string][]string)
	for rows.Next() {
		var callerID, calleeID string
		if err := rows.Scan(&callerID, &calleeID); err != nil {
			continue
		}
		graph[callerID] = append(graph[callerID], calleeID)
	}
	return graph, rows.Err()
}

// querySymbolNamesBatch fetches all symbol names in one query.
// Returns map[symbol_id]displayName where displayName is "Receiver.Name" or just "Name"
func querySymbolNamesBatch(db *sql.DB, repoRoot string) (map[string]string, error) {
	rows, err := db.Query(`
		SELECT id, name, COALESCE(receiver, '') as receiver
		FROM symbols
		WHERE file_path LIKE ? || '/%'
		  AND kind IN ('func', 'method')
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[string]string)
	for rows.Next() {
		var id, name, receiver string
		if err := rows.Scan(&id, &name, &receiver); err != nil {
			continue
		}
		if receiver != "" {
			// Extract type name from receiver like "*Store" -> "Store"
			typeName := strings.TrimPrefix(receiver, "*")
			names[id] = typeName + "." + name
		} else {
			names[id] = name
		}
	}
	return names, rows.Err()
}

// buildFlowPath constructs a flow string from an entry point using pre-fetched data.
func buildFlowPath(startID, startName, startReceiver string, callGraph map[string][]string, symbolNames map[string]string, maxDepth int) string {
	var path []string

	// Add entry point
	displayName := startName
	if startReceiver != "" {
		typeName := strings.TrimPrefix(startReceiver, "*")
		displayName = typeName + "." + startName
	}
	path = append(path, displayName)

	// Traverse call graph, preferring architecturally significant callees at each level
	visited := make(map[string]bool)
	visited[startID] = true
	currentID := startID

	for depth := 1; depth < maxDepth; depth++ {
		callees := callGraph[currentID]
		if len(callees) == 0 {
			break
		}

		nextID := pickBestCallee(callees, visited, symbolNames, callGraph)
		if nextID == "" {
			break
		}

		visited[nextID] = true
		name := symbolNames[nextID]
		if name != "" {
			path = append(path, name)
		}
		currentID = nextID
	}

	// Only return flows with at least 2 nodes
	if len(path) < 2 {
		return ""
	}

	return strings.Join(path, " -> ")
}

// GetChangeBoundaries identifies key packages by concern and returns top exported symbols.
// Categories: persistence (store), cli (cmd), output (output), query (query)
// Performance: Uses batch query with GROUP BY instead of per-package queries.
func GetChangeBoundaries(db *sql.DB, repoRoot string) (map[string][]string, error) {
	// Single query that groups symbols by concern based on package path
	// and returns top 5 exported symbols per concern
	rows, err := db.Query(`
		WITH ConcernSymbols AS (
			SELECT
				s.name,
				s.pkg_path,
				CASE
					WHEN s.pkg_path LIKE '%/store%' OR s.pkg_path LIKE '%store' THEN 'persistence'
					WHEN s.pkg_path LIKE '%/cmd%' OR s.pkg_path LIKE '%cmd' THEN 'cli'
					WHEN s.pkg_path LIKE '%/output%' OR s.pkg_path LIKE '%output' THEN 'output'
					WHEN s.pkg_path LIKE '%/query%' OR s.pkg_path LIKE '%query' THEN 'query'
					ELSE NULL
				END as concern,
				COUNT(r.id) as ref_count
			FROM symbols s
			LEFT JOIN refs r ON s.id = r.symbol_id
			WHERE s.file_path LIKE ? || '/%'
			  AND s.file_path NOT LIKE '%\_test.go' ESCAPE '\'
			  AND s.kind IN ('func', 'method', 'type', 'interface', 'struct')
			  AND s.name GLOB '[A-Z]*'
			GROUP BY s.id
		),
		RankedSymbols AS (
			SELECT
				name,
				concern,
				ref_count,
				ROW_NUMBER() OVER (PARTITION BY concern ORDER BY ref_count DESC) as rank
			FROM ConcernSymbols
			WHERE concern IS NOT NULL
		)
		SELECT concern, name
		FROM RankedSymbols
		WHERE rank <= 5
		ORDER BY concern, rank
	`, repoRoot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	boundaries := make(map[string][]string)
	for rows.Next() {
		var concern, name string
		if err := rows.Scan(&concern, &name); err != nil {
			continue
		}
		boundaries[concern] = append(boundaries[concern], name)
	}

	return boundaries, rows.Err()
}

// GetEntryPointDetails returns entry points with their doc comment and immediate callees.
// Uses the EntryPointRef type from types.go.
// Performance: Uses 2 batch queries (entry points, then callees) instead of N+1.
func GetEntryPointDetails(db *sql.DB, repoRoot string) ([]EntryPointRef, error) {
	// Query 1: Get entry points with their details
	rows, err := db.Query(`
		SELECT
			s.id,
			s.name,
			COALESCE(s.receiver, '') as receiver,
			s.file_path,
			s.line_start,
			COALESCE(s.doc, '') as doc
		FROM symbols s
		WHERE s.file_path LIKE ? || '/%'
		  AND s.file_path NOT LIKE '%\_test.go' ESCAPE '\'
		  AND s.kind IN ('func', 'method')
		  AND (
		      s.name = 'main'
		      OR s.name = 'Execute'
		      OR (s.name LIKE 'run%' AND s.file_path LIKE '%/cmd/%' AND s.name GLOB 'run[A-Z]*')
		  )
		ORDER BY
		  CASE
		    WHEN s.name = 'main' THEN 1
		    WHEN s.name = 'Execute' THEN 2
		    ELSE 3
		  END,
		  s.name
	`, repoRoot)
	if err != nil {
		return nil, err
	}

	// Collect entry points and their IDs for callee lookup
	type epWithID struct {
		id  string
		ref EntryPointRef
	}
	var entryPoints []epWithID

	for rows.Next() {
		var ep epWithID
		var receiver, doc string
		if err := rows.Scan(&ep.id, &ep.ref.Name, &receiver, &ep.ref.File, &ep.ref.Line, &doc); err != nil {
			continue
		}

		// Format display name with receiver
		if receiver != "" {
			typeName := strings.TrimPrefix(receiver, "*")
			ep.ref.Name = typeName + "." + ep.ref.Name
		}

		// Make file path relative
		ep.ref.File = strings.TrimPrefix(ep.ref.File, repoRoot+"/")

		// Extract first sentence of doc comment as purpose
		if doc != "" {
			ep.ref.Purpose = ExtractFirstSentence(doc)
		}

		entryPoints = append(entryPoints, ep)
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}

	if len(entryPoints) == 0 {
		return nil, nil
	}

	// Query 2: Batch fetch callees for all entry points
	// Build a single query with IN clause for all entry point IDs
	ids := make([]string, len(entryPoints))
	for i, ep := range entryPoints {
		ids[i] = ep.id
	}

	// Fetch all callees (no LIMIT) — we rank by significance in Go
	calleeQuery := `
		SELECT DISTINCT cg.caller_id, callee.name, COALESCE(callee.receiver, '') as receiver
		FROM call_graph cg
		JOIN symbols callee ON cg.callee_id = callee.id
		WHERE cg.caller_id IN (` + placeholders(len(ids)) + `)
		  AND callee.file_path LIKE ? || '/%'
		ORDER BY caller_id
	`

	// Build args: IDs first, then repoRoot for LIKE clause
	args := make([]interface{}, len(ids)+1)
	for i, id := range ids {
		args[i] = id
	}
	args[len(ids)] = repoRoot

	calleeRows, err := db.Query(calleeQuery, args...)
	if err != nil {
		return nil, err
	}
	defer calleeRows.Close()

	// Build callee map (deduplicated)
	calleeMap := make(map[string][]string)
	calleeSeen := make(map[string]map[string]bool)
	for calleeRows.Next() {
		var callerID, name, receiver string
		if err := calleeRows.Scan(&callerID, &name, &receiver); err != nil {
			continue
		}
		displayName := name
		if receiver != "" {
			typeName := strings.TrimPrefix(receiver, "*")
			displayName = typeName + "." + name
		}
		if calleeSeen[callerID] == nil {
			calleeSeen[callerID] = make(map[string]bool)
		}
		if !calleeSeen[callerID][displayName] {
			calleeSeen[callerID][displayName] = true
			calleeMap[callerID] = append(calleeMap[callerID], displayName)
		}
	}

	// Backfill purposes from Cobra Short descriptions when doc comments are empty
	cobraShorts := extractCobraShorts(repoRoot)

	// Build final result — rank callees by architectural significance, keep top 5
	var results []EntryPointRef
	for _, ep := range entryPoints {
		ep.ref.Callees = rankCallees(calleeMap[ep.id], 5)
		if ep.ref.Purpose == "" && cobraShorts != nil {
			ep.ref.Purpose = cobraShorts[ep.ref.Name]
		}
		// Fallback for sub-handlers that don't have their own Cobra command
		if ep.ref.Purpose == "" {
			ep.ref.Purpose = subHandlerPurpose(ep.ref.Name)
		}
		results = append(results, ep.ref)
	}

	return results, nil
}

// rankCallees scores callees by architectural significance and returns the top N.
// Deprioritizes infrastructure (DB, Close, Error), format helpers, and boolean checks.
// Promotes cross-package calls, exported symbols, and domain-specific operations.
func rankCallees(callees []string, limit int) []string {
	if len(callees) <= limit {
		return callees
	}

	type scored struct {
		name  string
		score int
	}

	var items []scored
	for _, c := range callees {
		s := calleeSignificance(c)
		items = append(items, scored{name: c, score: s})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].name < items[j].name
	})

	result := make([]string, 0, limit)
	for i := 0; i < limit && i < len(items); i++ {
		result = append(result, items[i].name)
	}
	return result
}

// calleeSignificance scores a callee name for architectural importance.
func calleeSignificance(name string) int {
	score := 5 // baseline

	// Infrastructure terminals — low value
	bare := name
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		bare = name[idx+1:]
	}
	if terminalMethods[bare] {
		score -= 4
	}

	// Method calls (Type.Method) = cross-package significance
	if strings.Contains(name, ".") {
		score += 3
	}

	// Exported symbols
	if len(bare) > 0 && bare[0] >= 'A' && bare[0] <= 'Z' {
		score += 2
	}

	// Boolean helpers (isX, hasX) — low architectural signal
	lower := strings.ToLower(bare)
	if strings.HasPrefix(lower, "is") || strings.HasPrefix(lower, "has") {
		score -= 2
	}

	return score
}

// subHandlerPurpose returns a purpose string for sub-handler functions that lack
// their own Cobra command and doc comment. Returns "" if unknown.
func subHandlerPurpose(name string) string {
	purposes := map[string]string{
		"runBatchEdit":  "Apply multiple AST-aware edits from JSON stdin",
		"runDepsSingle": "Show bidirectional dependencies for one package",
		"runDepsTree":   "Show full internal dependency graph with cycle detection",
	}
	return purposes[name]
}

// flowPriority returns a priority score for flow selection. Lower = more important.
// runDef and runRefs get highest core priority — they're the bread-and-butter commands.
func flowPriority(name string) int {
	switch name {
	case epMain, epExecute:
		return 0 // always include
	case fnRunDef, fnRunRefs:
		return 1 // most-used navigation
	case "runCallers", "runCallees", "runSearch":
		return 2 // other core navigation
	case "runIndex", fnRunContext, "runPack", "runImpl":
		return 3 // important operations
	case "runEdit", "runImpact", "runTests", "runSim", "runDeps":
		return 4 // secondary commands
	default:
		return 5 // utility/admin
	}
}

// terminalMethods are infrastructure methods that don't reveal architectural intent.
// Flows that end at these tell Claude the plumbing, not the purpose.
var terminalMethods = map[string]bool{
	"DB": true, "Close": true, "String": true, "Error": true, "Err": true,
	"Bytes": true, "Len": true, "Reset": true,
}

// pickBestCallee selects the most architecturally significant callee from candidates.
// Prefers cross-package method calls and exported symbols. Avoids infrastructure
// terminals (DB, Close) and prefers callees that have their own callees (deeper paths).
func pickBestCallee(callees []string, visited map[string]bool, symbolNames map[string]string, callGraph map[string][]string) string {
	var bestID string
	var bestScore int

	for _, calleeID := range callees {
		if visited[calleeID] {
			continue
		}
		name := symbolNames[calleeID]
		if name == "" {
			continue
		}
		// Skip stdlib utility calls
		if strings.HasPrefix(name, "fmt.") || strings.HasPrefix(name, "log.") || strings.HasPrefix(name, "strings.") {
			continue
		}

		score := 1
		// Extract bare function name
		parts := strings.SplitN(name, ".", 2)
		checkName := parts[len(parts)-1]
		lower := strings.ToLower(checkName)

		// Deprioritize infrastructure terminals — they reveal plumbing, not intent
		if terminalMethods[checkName] {
			score -= 4
		}
		// Deprioritize error/write infrastructure — these are output paths, not domain logic
		if strings.HasPrefix(checkName, "Write") || strings.HasPrefix(checkName, "New") && strings.HasSuffix(checkName, "Error") {
			score -= 3
		}
		// Boost domain operations (Lookup, Find, Search, Resolve, Extract, Detect)
		if strings.HasPrefix(checkName, "Lookup") || strings.HasPrefix(checkName, "Find") ||
			strings.HasPrefix(checkName, "Search") || strings.HasPrefix(checkName, "Resolve") ||
			strings.HasPrefix(checkName, "Extract") || strings.HasPrefix(checkName, "Detect") {
			score += 4
		}
		// Prefer method calls (Type.Method pattern — cross-package significance)
		if strings.Contains(name, ".") {
			score += 2
		}
		// Prefer exported names
		if len(checkName) > 0 && checkName[0] >= 'A' && checkName[0] <= 'Z' {
			score += 2
		}
		// Deprioritize boolean helpers (isX, hasX)
		if strings.HasPrefix(lower, "is") || strings.HasPrefix(lower, "has") {
			score -= 2
		}
		// Prefer callees that have their own callees (deeper paths available)
		if _, hasCallees := callGraph[calleeID]; hasCallees {
			score += 3
		}

		if score > bestScore {
			bestScore = score
			bestID = calleeID
		}
	}

	return bestID
}

// placeholders generates SQL placeholders like "?,?,?" for IN clauses.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// extractCobraShorts scans cmd/*.go files for Cobra Short descriptions and returns
// a map from runX function name to the Short string. Uses the convention that each
// cmd file has one cobra.Command with a Short field and one runX handler.
func extractCobraShorts(repoRoot string) map[string]string {
	cmdDir := filepath.Join(repoRoot, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return nil
	}

	shortRe := regexp.MustCompile(`Short:\s*"([^"]+)"`)
	runRe := regexp.MustCompile(`^func (run[A-Z]\w*)\(`)

	result := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		f, err := os.Open(filepath.Join(cmdDir, entry.Name()))
		if err != nil {
			continue
		}

		var short, runFunc string
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if m := shortRe.FindStringSubmatch(line); m != nil && short == "" {
				short = m[1]
			}
			if m := runRe.FindStringSubmatch(line); m != nil && runFunc == "" {
				runFunc = m[1]
			}
		}
		f.Close()

		if short != "" && runFunc != "" {
			result[runFunc] = short
		}
	}
	return result
}

// compressToCommandTable converts verbose entry point details into a compact command table.
// Detects the boilerplate callee pattern (callees appearing in >40% of commands),
// strips it from individual commands, and only includes notable deviations.
func compressToCommandTable(details []EntryPointRef) *CommandTable {
	if len(details) == 0 {
		return nil
	}

	// Filter out trivial entry points (main, Execute — not real commands)
	var commands []EntryPointRef
	for _, ep := range details {
		if ep.Name == epMain || ep.Name == epExecute {
			continue
		}
		commands = append(commands, ep)
	}
	if len(commands) == 0 {
		return nil
	}

	// Count callee frequency across all commands (deduplicated per command)
	calleeFreq := make(map[string]int)
	for _, cmd := range commands {
		seen := make(map[string]bool)
		for _, c := range cmd.Callees {
			if !seen[c] {
				seen[c] = true
				calleeFreq[c]++
			}
		}
	}

	// Boilerplate = callees appearing in >25% of commands (minimum 2)
	threshold := len(commands) * 25 / 100
	if threshold < 2 {
		threshold = 2
	}
	boilerplate := make(map[string]bool)
	var boilerplateList []string
	for name, count := range calleeFreq {
		if count >= threshold {
			boilerplate[name] = true
			boilerplateList = append(boilerplateList, name)
		}
	}
	sort.Strings(boilerplateList)

	// Build compressed command entries
	var entries []CommandEntry
	for _, cmd := range commands {
		entry := CommandEntry{
			Name:    cmd.Name,
			Purpose: cmd.Purpose,
		}

		// Only include non-boilerplate callees (deduplicated)
		seen := make(map[string]bool)
		var notable []string
		for _, c := range cmd.Callees {
			if !boilerplate[c] && !seen[c] {
				seen[c] = true
				notable = append(notable, c)
			}
		}
		if len(notable) > 0 {
			entry.Callees = notable
		}

		entries = append(entries, entry)
	}

	return &CommandTable{
		BoilerplatePattern: boilerplateList,
		Commands:           entries,
	}
}
