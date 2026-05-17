package cmd

import (
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	typesAt string
)

var typesCmd = &cobra.Command{
	Use:     "types [type-name]",
	Short:   "Show type relationships",
	GroupID: categoryGraph,
	Long: `Displays type information including methods, embeds, and fields.

Output includes:
  - methods: All methods with this type as receiver
  - embeds: Embedded types (v1: best-effort detection)
  - fields: Struct fields with types and tags
  - implements: Interface satisfaction (v1: partial/future)

Note: Full interface satisfaction detection requires type-checker
analysis and is planned for v2.

Examples:
  snipe types Store                    # By name
  snipe types --at internal/store.go:42  # By position
  snipe types query.SymbolRow          # Qualified name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTypes,
}

func init() {
	typesCmd.Flags().StringVar(&typesAt, "at", "", "Position to look up (file:line:col)")
	rootCmd.AddCommand(typesCmd)
}

func runTypes(cmd *cobra.Command, args []string) error {
	start := time.Now()

	compact, _, _, _, _, _ := GetOutputConfig()
	w := output.NewWriter(os.Stdout, compact, GetOutputFormat())

	if len(args) == 0 && typesAt == "" {
		return w.WriteError(cmdNameTypes, &output.Error{
			Code:    output.ErrInternal,
			Message: "provide a type name or --at position",
		})
	}

	s, dir, err := OpenStore(w, cmdNameTypes)
	if err != nil {
		return err
	}
	defer s.Close()

	// Package-path shortcut: `snipe types internal/store` lists all types
	// declared in that package. Falls through to name lookup when the path
	// matches no package, so a (rare) slash-bearing symbol name still works.
	if typesAt == "" && len(args) == 1 && looksLikePkgPath(args[0]) {
		if handled, err := runTypesForPackage(w, s, dir, args[0], start); handled {
			return err
		}
	}

	var symbolID string
	var queryInfo map[string]string

	if typesAt != "" {
		pos, err := query.ParsePosition(typesAt)
		if err != nil {
			return w.WriteError(cmdNameTypes, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if !filepath.IsAbs(pos.File) {
			pos.File = filepath.Join(dir, pos.File)
		}

		symbolID, err = query.ResolvePosition(s.DB(), pos)
		if err != nil {
			return w.WriteError(cmdNameTypes, &output.Error{
				Code:    output.ErrNotFound,
				Message: err.Error(),
			})
		}
		queryInfo = map[string]string{"at": typesAt}
	} else {
		name := args[0]

		// Check for hex ID
		if len(name) == 16 {
			if _, err := hex.DecodeString(name); err == nil {
				symbolID = name
				queryInfo = map[string]string{"id": name}
				goto getTypes
			}
		}

		// Check for file:Symbol syntax
		if idx := strings.LastIndex(name, ":"); idx > 0 && !strings.Contains(name[idx:], "/") {
			filePart := name[:idx]
			symbolPart := name[idx+1:]
			if symbolPart != "" && !strings.Contains(symbolPart, ":") {
				symbols, err := query.LookupByNameInFile(s.DB(), symbolPart, filePart)
				if err != nil {
					return w.WriteError(cmdNameTypes, &output.Error{
						Code:    output.ErrInternal,
						Message: err.Error(),
					})
				}
				if len(symbols) == 1 {
					symbolID = symbols[0].ID
					queryInfo = map[string]string{flagSymbol: symbolPart, flagFile: filePart}
					goto getTypes
				} else if len(symbols) > 1 {
					candidates := make([]output.Candidate, len(symbols))
					for i := range symbols {
						sym := &symbols[i]
						candidates[i] = sym.ToCandidate()
					}
					return w.WriteError(cmdNameTypes, output.NewAmbiguousError(name, candidates))
				}
			}
		}

		// Regular lookup
		symbols, err := query.LookupByName(s.DB(), name)
		if err != nil {
			return w.WriteError(cmdNameTypes, &output.Error{
				Code:    output.ErrInternal,
				Message: err.Error(),
			})
		}

		if len(symbols) == 0 {
			maxDist := query.DefaultMaxDistance(name)
			suggestions, err := query.FindSimilarSymbols(s.DB(), name, maxDist, 3)
			if err != nil {
				return w.WriteError(cmdNameTypes, output.NewNotFoundError(name))
			}
			return w.WriteError(cmdNameTypes, output.NewNotFoundError(name, suggestions...))
		}

		// Filter to type-like symbols
		var typeSymbols []query.SymbolRow
		for i := range symbols {
			sym := &symbols[i]
			if isTypeKind(sym.Kind) {
				typeSymbols = append(typeSymbols, *sym)
			}
		}

		if len(typeSymbols) == 0 {
			return w.WriteError(cmdNameTypes, &output.Error{
				Code:    output.ErrNotFound,
				Message: "'" + name + "' is not a type (found " + symbols[0].Kind + ")",
			})
		}

		if len(typeSymbols) > 1 {
			candidates := make([]output.Candidate, len(typeSymbols))
			for i := range typeSymbols {
				candidates[i] = typeSymbols[i].ToCandidate()
			}
			return w.WriteError(cmdNameTypes, output.NewAmbiguousError(name, candidates))
		}

		symbolID = typeSymbols[0].ID
		queryInfo = map[string]string{flagSymbol: name}
	}

getTypes:
	typeInfo, err := query.GetTypeInfo(s.DB(), symbolID)
	if err != nil {
		return w.WriteError(cmdNameTypes, &output.Error{
			Code:    output.ErrInternal,
			Message: err.Error(),
		})
	}

	// Build response
	result := output.TypesResult{
		Symbol:    typeInfo.Symbol.Name,
		Kind:      typeInfo.Symbol.Kind,
		File:      typeInfo.Symbol.FilePathRel,
		Signature: typeInfo.Symbol.Signature.String,
		Doc:       typeInfo.Symbol.Doc.String,
		Implements: output.TypesImplementsOut{
			Status: typeInfo.Implements.Status,
			Note:   typeInfo.Implements.Note,
		},
	}

	// Add methods
	for _, m := range typeInfo.Methods {
		result.Methods = append(result.Methods, output.TypesMethodOut{
			Name:      m.Name,
			Signature: m.Signature,
			File:      m.File,
			Line:      m.Line,
		})
	}

	// Add embeds
	for _, e := range typeInfo.Embeds {
		result.Embeds = append(result.Embeds, output.TypesEmbedOut{
			TypeName:  e.TypeName,
			FieldName: e.FieldName,
			File:      e.File,
		})
	}

	// Add fields
	for _, f := range typeInfo.Fields {
		result.Fields = append(result.Fields, output.TypesFieldOut{
			Name:     f.Name,
			TypeExpr: f.TypeExpr,
			Tag:      f.Tag,
		})
	}

	staleFiles := query.CheckPathStaleness(s.DB(), dir, []string{typeInfo.Symbol.FilePath})

	resp := output.Response[output.TypesResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  []output.TypesResult{result},
		Meta: output.Meta{
			Command:    cmdNameTypes,
			Query:      queryInfo,
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      1,
			StaleFiles: staleFiles,
		},
	}

	return w.WriteResponse(resp)
}

// looksLikePkgPath reports whether s is plausibly a Go package path
// (slash present, no leading dot, not obviously a filename with a
// Go-source extension). Conservative: returns true only when the
// path has at least one `/` and none of the trailing path segment
// contains `.go` — otherwise it might be a file like `foo/bar.go`.
func looksLikePkgPath(s string) bool {
	if !strings.Contains(s, "/") {
		return false
	}
	if strings.HasSuffix(s, ".go") {
		return false
	}
	// Receiver syntax like "(*Foo).Bar" or hex IDs never contain slashes;
	// this is a cheap sanity filter only.
	return !strings.ContainsAny(s, "()")
}

// runTypesForPackage lists every struct/interface/type-alias declared in
// a package. Returns handled=false when the package has no matching symbols
// so the caller can fall back to the regular symbol-name lookup.
func runTypesForPackage(w *output.Writer, s interface {
	DB() *sql.DB
}, dir, pkgPath string, start time.Time) (bool, error) {
	// Wide limit — we filter to types only, which is typically a small subset.
	symbols, err := query.FindPackageSymbols(s.DB(), pkgPath, 500, 0)
	if err != nil {
		return false, nil //nolint:nilerr // deliberate: let caller fall through to name-based lookup
	}

	var typeSymbols []query.SymbolRow
	for i := range symbols {
		sym := &symbols[i]
		if isTypeKind(sym.Kind) {
			typeSymbols = append(typeSymbols, *sym)
		}
	}
	if len(typeSymbols) == 0 {
		return false, nil
	}

	results := make([]output.TypesResult, 0, len(typeSymbols))
	for i := range typeSymbols {
		sym := &typeSymbols[i]
		info, err := query.GetTypeInfo(s.DB(), sym.ID)
		if err != nil {
			continue
		}
		results = append(results, buildTypesResult(info))
	}

	resp := output.Response[output.TypesResult]{
		Protocol: output.ProtocolVersion,
		Ok:       true,
		Results:  results,
		Meta: output.Meta{
			Command:    cmdNameTypes,
			Query:      map[string]string{flagPkg: pkgPath},
			RepoRoot:   dir,
			IndexState: query.CheckIndexState(s.DB(), dir, Version),
			Ms:         time.Since(start).Milliseconds(),
			Total:      len(results),
		},
	}

	return true, w.WriteResponse(resp)
}

// buildTypesResult converts a query.TypeInfo into the output rendering form.
func buildTypesResult(info *query.TypeInfo) output.TypesResult {
	r := output.TypesResult{
		Symbol:    info.Symbol.Name,
		Kind:      info.Symbol.Kind,
		File:      info.Symbol.FilePathRel,
		Signature: info.Symbol.Signature.String,
		Doc:       info.Symbol.Doc.String,
		Implements: output.TypesImplementsOut{
			Status: info.Implements.Status,
			Note:   info.Implements.Note,
		},
	}
	for _, m := range info.Methods {
		r.Methods = append(r.Methods, output.TypesMethodOut{
			Name:      m.Name,
			Signature: m.Signature,
			File:      m.File,
			Line:      m.Line,
		})
	}
	for _, e := range info.Embeds {
		r.Embeds = append(r.Embeds, output.TypesEmbedOut{
			TypeName:  e.TypeName,
			FieldName: e.FieldName,
			File:      e.File,
		})
	}
	for _, f := range info.Fields {
		r.Fields = append(r.Fields, output.TypesFieldOut{
			Name:     f.Name,
			TypeExpr: f.TypeExpr,
			Tag:      f.Tag,
		})
	}
	return r
}
