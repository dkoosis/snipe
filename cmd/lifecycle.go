package cmd

import (
	"database/sql"
	"encoding/hex"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/lifecycle"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var lifecycleIncludeTests bool
var lifecycleCallerDepth int

var lifecycleCmd = &cobra.Command{
	Use:     "lifecycle <Type>",
	Short:   "Trace every function creating, mutating, reading, or deleting a type",
	GroupID: "advanced",
	Long: `Traces all functions that create, mutate, read, or delete instances of a type,
grouped by CRUD role.

Classification is heuristic: evidence from the ref snippet (struct literal,
new(), make(), store.Delete) outranks name-based heuristics (NewX, DeleteX).
Each function carries a Signal showing the matching rule, so misclassifications
are debuggable at a glance.

Accepts a type name or 16-char hex symbol ID (chainable from prior
disambiguation output).

Examples:
  snipe lifecycle Nug                       # Full lifecycle for type Nug
  snipe lifecycle f2efb7b35d08313b          # By hex ID (resolves ambiguity)
  snipe lifecycle Store --format json       # Machine-readable output
  snipe lifecycle Nug --include-tests       # Include test file refs in groups`,
	Args: cobra.ExactArgs(1),
	RunE: runLifecycle,
}

func init() {
	lifecycleCmd.Flags().BoolVar(&lifecycleIncludeTests, "include-tests", false,
		"Include _test.go and generated files in CRUD groups (default: bucketed separately)")
	lifecycleCmd.Flags().IntVar(&lifecycleCallerDepth, "depth", 3,
		"Caller-chain walk depth per classified function (0 = disabled)")
	rootCmd.AddCommand(lifecycleCmd)
}

func runLifecycle(cmd *cobra.Command, args []string) error {
	start := time.Now()

	_, lim, off, _, _, _ := GetOutputConfig()

	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	typeName := args[0]

	s, dir, err := OpenStore(w, "lifecycle")
	if err != nil {
		return err
	}
	defer s.Close()

	var sym query.SymbolRow
	resolved := false

	// Auto-detect 16-char hex ID (chainable from prior disambiguation output).
	if len(typeName) == 16 {
		if _, hexErr := hex.DecodeString(typeName); hexErr == nil {
			row, lookupErr := query.LookupByID(s.DB(), typeName)
			if lookupErr != nil {
				return w.WriteError("lifecycle", &output.Error{
					Code:    output.ErrInternal,
					Message: lookupErr.Error(),
				})
			}
			if row != nil {
				sym = *row
				typeName = row.Name
				resolved = true
			}
		}
	}

	if !resolved {
		symbols, err := query.LookupByName(s.DB(), typeName)
		if err != nil {
			return w.WriteError("lifecycle", &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}
		if len(symbols) == 0 {
			return w.WriteError("lifecycle", output.NewNotFoundError(typeName))
		}
		if len(symbols) > 1 {
			candidates := make([]output.Candidate, len(symbols))
			for i, s := range symbols {
				candidates[i] = s.ToCandidate()
			}
			return w.WriteError("lifecycle", output.NewAmbiguousError(typeName, candidates))
		}
		sym = symbols[0]
	}

	// Fetch all refs. Use a large limit; lifecycle is a holistic view.
	refRows, err := query.FindRefs(s.DB(), sym.ID, 10000, 0)
	if err != nil {
		return w.WriteError("lifecycle", &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// The indexer does not attach enclosing_id to refs on a function's
	// signature line (e.g. the return-type ref of `func F() *T`). Recover
	// these by reassigning any enclosing-less ref to the innermost function
	// whose line range covers it in the same file.
	reattachSignatureRefs(s.DB(), refRows)

	refs := lifecycle.FromRefRows(refRows)
	classifications := lifecycle.Classify(typeName, refs)

	result := buildLifecycleResult(s.DB(), sym, refRows, classifications, lifecycleIncludeTests, lifecycleCallerDepth)

	tokenTruncated := false
	if maxTok := GetMaxTokens(); maxTok > 0 {
		result, tokenTruncated = output.TruncateLifecycleToTokenBudget(result, maxTok)
	}

	meta := output.Meta{
		Command:   "lifecycle",
		Query:     map[string]string{"type": typeName},
		RepoRoot:  dir,
		Ms:        time.Since(start).Milliseconds(),
		Total:     1,
		Offset:    off,
		Limit:     lim,
		Truncated: tokenTruncated,
	}

	return w.WriteResponse(output.Response[output.LifecycleResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.LifecycleResult{result},
		Meta:     meta,
	})
}

// roleOrder is the canonical output ordering.
var roleOrder = []lifecycle.Role{
	lifecycle.RoleCreate,
	lifecycle.RoleMutate,
	lifecycle.RoleRead,
	lifecycle.RoleDelete,
	lifecycle.RoleUnknown,
}

func buildLifecycleResult(
	db *sql.DB,
	sym query.SymbolRow,
	refRows []query.RefRow,
	classifications []lifecycle.Classification,
	includeTests bool,
	callerDepth int,
) output.LifecycleResult {
	buckets := make(map[lifecycle.Role][]output.LifecycleFunction)
	testBucket := []output.LifecycleFunction{}
	funcCount := 0
	testRefCount := 0

	for _, c := range classifications {
		fn := output.LifecycleFunction{
			ID:      c.EnclosingID,
			Name:    displayName(c.EnclosingName),
			File:    c.FileRel,
			Line:    c.Line,
			Signal:  c.Signal,
			Mixed:   rolesToStrings(c.Mixed),
			Callers: buildCallerChain(db, c.EnclosingID, callerDepth),
		}
		if c.IsTestFile && !includeTests {
			testBucket = append(testBucket, fn)
			testRefCount++
			continue
		}
		buckets[c.Role] = append(buckets[c.Role], fn)
		funcCount++
	}

	// Sort within each bucket for stable output.
	for role := range buckets {
		sortLifecycleFuncs(buckets[role])
	}
	sortLifecycleFuncs(testBucket)

	groups := make([]output.LifecycleGroup, 0, len(roleOrder)+1)
	for _, role := range roleOrder {
		funcs := buckets[role]
		groups = append(groups, output.LifecycleGroup{
			Role:  string(role),
			Count: len(funcs),
			Funcs: funcs,
		})
	}
	if len(testBucket) > 0 {
		groups = append(groups, output.LifecycleGroup{
			Role:  "Tests",
			Count: len(testBucket),
			Funcs: testBucket,
		})
	}

	return output.LifecycleResult{
		Type:         sym.Name,
		TypeID:       sym.ID,
		TypeFile:     sym.FilePathRel,
		TypeLine:     sym.LineStart,
		TypeKind:     sym.Kind,
		TotalRefs:    len(refRows),
		FunctionRefs: funcCount,
		TestRefs:     testRefCount,
		Groups:       groups,
	}
}

func displayName(name string) string {
	if name == "" {
		return "(file-scope)"
	}
	return name
}

func rolesToStrings(roles []lifecycle.Role) []string {
	if len(roles) == 0 {
		return nil
	}
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// reattachSignatureRefs fixes refs that point at the return type or parameter
// of a function declaration but were not attributed to that function by the
// indexer. For each file, fetch all func/method symbols once, then bind any
// orphan ref to the narrowest enclosing function.
func reattachSignatureRefs(db *sql.DB, refs []query.RefRow) {
	// Collect files with orphan refs.
	wantFiles := map[string]bool{}
	for _, r := range refs {
		if !r.EnclosingID.Valid {
			wantFiles[r.FilePath] = true
		}
	}
	if len(wantFiles) == 0 {
		return
	}

	type funcRange struct {
		id        string
		name      string
		signature sql.NullString
		kind      string
		start     int
		end       int
	}
	byFile := make(map[string][]funcRange)
	for file := range wantFiles {
		rows, err := db.Query(`
			SELECT id, name, kind, signature, line_start, line_end
			FROM symbols
			WHERE file_path = ? AND kind IN ('func', 'method')
		`, file)
		if err != nil {
			continue
		}
		for rows.Next() {
			var fr funcRange
			if err := rows.Scan(&fr.id, &fr.name, &fr.kind, &fr.signature, &fr.start, &fr.end); err != nil {
				continue
			}
			byFile[file] = append(byFile[file], fr)
		}
		rows.Close()
	}

	for i := range refs {
		if refs[i].EnclosingID.Valid {
			continue
		}
		cands := byFile[refs[i].FilePath]
		var best *funcRange
		bestWidth := 1<<31 - 1
		for j := range cands {
			c := cands[j]
			if refs[i].Line < c.start || refs[i].Line > c.end {
				continue
			}
			w := c.end - c.start
			if w < bestWidth {
				best = &cands[j]
				bestWidth = w
			}
		}
		if best == nil {
			continue
		}
		refs[i].EnclosingID = sql.NullString{String: best.id, Valid: true}
		refs[i].EnclosingName = best.name
		refs[i].EnclosingKind = best.kind
		if best.signature.Valid {
			refs[i].EnclosingSignature = best.signature.String
		}
	}
}

func buildCallerChain(db *sql.DB, symbolID string, depth int) []output.LifecycleCallerNode {
	if symbolID == "" || depth <= 0 {
		return nil
	}
	nodes := lifecycle.WalkCallers(db, symbolID, depth)
	if len(nodes) == 0 {
		return nil
	}
	out := make([]output.LifecycleCallerNode, len(nodes))
	for i, n := range nodes {
		out[i] = output.LifecycleCallerNode{
			ID:    n.ID,
			Name:  n.Name,
			File:  n.File,
			Depth: n.Depth,
		}
	}
	return out
}

func sortLifecycleFuncs(fns []output.LifecycleFunction) {
	sort.Slice(fns, func(i, j int) bool {
		if fns[i].File != fns[j].File {
			return fns[i].File < fns[j].File
		}
		return fns[i].Line < fns[j].Line
	})
}
