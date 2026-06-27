package cmd

import (
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dkoosis/snipe/internal/lifecycle"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var lifecycleIncludeTests bool
var lifecycleCallerDepth int
var lifecyclePkg string
var lifecycleAt string

func runLifecycle(args []string) error {
	start := time.Now()

	_, lim, off, _, _, _ := GetOutputConfig()

	w := output.NewWriter(os.Stdout, GetOutputFormat())

	if len(args) == 0 && lifecycleAt == "" {
		return w.WriteError("lifecycle", &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a type name, hex ID, or --at position",
		})
	}

	var typeName string
	if len(args) == 1 {
		typeName = args[0]
	}

	s, dir, err := OpenStore(w, "lifecycle")
	if err != nil {
		return err
	}
	defer s.Close()

	var sym query.SymbolRow
	resolved := false

	// --at position: resolve to a symbol id, then load it.
	if lifecycleAt != "" {
		pos, perr := query.ParsePosition(lifecycleAt)
		if perr != nil {
			return w.WriteError("lifecycle", &output.Error{
				Code:    output.ErrInternal,
				Message: perr.Error(),
			})
		}
		if !filepath.IsAbs(pos.File) {
			pos.File = filepath.Join(dir, pos.File)
		}
		id, rerr := query.ResolvePosition(s.DB(), pos)
		if rerr != nil {
			return w.WriteError("lifecycle", &output.Error{
				Code:    output.ErrNotFound,
				Message: "no symbol found at " + lifecycleAt,
			})
		}
		row, lookupErr := query.LookupByID(s.DB(), id)
		if lookupErr != nil || row == nil {
			return w.WriteError("lifecycle", &output.Error{
				Code:    output.ErrNotFound,
				Message: "symbol id " + id + " not found",
			})
		}
		sym = *row
		typeName = row.Name
		resolved = true
	}

	// Auto-detect 16-char hex ID (chainable from prior disambiguation output).
	if !resolved && len(typeName) == 16 {
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
		var symbols []query.SymbolRow
		var err error
		if lifecyclePkg != "" {
			symbols, err = query.LookupByNameInPkg(s.DB(), typeName, lifecyclePkg)
		} else {
			symbols, err = query.LookupByName(s.DB(), typeName)
		}
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
			for i := range symbols {
				s := &symbols[i]
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

	refs := lifecycle.FromRefRows(refRows)
	classifications := lifecycle.Classify(typeName, refs)

	summary := GetResponseFormat() == FormatSummary
	callerDepth := lifecycleCallerDepth
	if summary {
		callerDepth = 0 // skip caller-chain walk; summary suppresses them anyway
	}

	result := buildLifecycleResult(s.DB(), sym, refRows, classifications, lifecycleIncludeTests, callerDepth)
	result.Summary = summary

	tokenTruncated := false
	if maxTok := GetMaxTokens(); maxTok > 0 {
		result, tokenTruncated = output.TruncateLifecycleToTokenBudget(result, maxTok)
	}

	meta := output.Meta{
		Command:   "lifecycle",
		Query:     map[string]string{cmdKindType: typeName},
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
	lifecycle.RoleTypeUse,
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
