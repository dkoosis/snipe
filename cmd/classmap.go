package cmd

// sn-l1kh.9: class map — a D2 class diagram of Go types (fields, methods,
// embeds, implements edges), mirroring datastore-map's shape (repo-wide
// auto-discovery, no positional arg) but for Go type structure instead of
// SQL access patterns.
//
// Field/method/embed extraction does NOT go through query.GetTypeInfoFromSymbol:
// that helper's Fields come from a `kind = 'field'` symbols query, but the
// indexer never emits field-kind rows (KindField is declared, never assigned
// in internal/index/symbols.go) — it always returns empty. Rather than widen
// the indexer's blast radius (new symbol rows would enter every bare-name
// lookup, search, and lifecycle classification repo-wide) for a single
// comprehension diagram, this file parses each candidate's own source file
// directly with go/parser — no go/packages, no type-checking, just the field
// list and interface method list AS WRITTEN, which is what a comprehension
// view should show anyway. Method lookups (concrete methods on structs)
// reuse query.GetMethodsForType, which DOES work (it matches by receiver
// against real function symbols, not the broken field path). Implements
// edges reuse query.FindImplementers, the same lookup runDiagramSeams uses.
import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/diagram"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// classMember is one field or method line rendered inside a class node body.
type classMember struct {
	Name     string
	Type     string // field type, or "(params) results" for a method — source text as written, unresolved
	Exported bool
}

// classType is one struct or interface rendered as a D2 class node.
type classType struct {
	Sym     query.SymbolRow
	Fields  []classMember
	Methods []classMember
	Embeds  []string // embedded type text as written, e.g. "*Base", "sync.Mutex", "io.Reader"
}

// richness ranks candidates for the Top-N cut: the more a type's own
// definition conveys, the more it earns a place in a size-bounded
// comprehension diagram — same spirit as arch's PageRank trim and seams'
// fan-in score, just computed from what we already parsed instead of an
// extra DB round trip.
func (c *classType) richness() int {
	return len(c.Fields) + len(c.Methods) + len(c.Embeds)
}

// findClassCandidates enumerates in-repo structs and interfaces (excludes
// _test.go-only types, which aren't part of the shipped type graph).
func findClassCandidates(db *sql.DB) ([]query.SymbolRow, error) {
	rows, err := db.Query(`
		SELECT id, name, kind, file_path, file_path_rel, pkg_path, line_start, line_end
		FROM symbols
		WHERE kind IN (?, ?)
		  AND file_path_rel != ''
		  AND file_path NOT LIKE '%$_test.go' ESCAPE '$'
		ORDER BY name, file_path_rel
	`, cmdKindStruct, cmdKindInterface)
	if err != nil {
		return nil, fmt.Errorf("query struct/interface candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []query.SymbolRow
	for rows.Next() {
		var s query.SymbolRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Kind, &s.FilePath, &s.FilePathRel, &s.PkgPath, &s.LineStart, &s.LineEnd); err != nil {
			return nil, fmt.Errorf("scan struct/interface candidate: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// parseClassMembers fills Fields/Methods/Embeds for every candidate,
// grouping by file so each file is parsed exactly once regardless of how
// many candidate types it declares.
func parseClassMembers(candidates []*classType) error {
	byFile := make(map[string][]*classType)
	for _, c := range candidates {
		byFile[c.Sym.FilePath] = append(byFile[c.Sym.FilePath], c)
	}

	for filePath, types := range byFile {
		byName := make(map[string]*classType, len(types))
		for _, t := range types {
			byName[t.Sym.Name] = t
		}

		src, err := os.ReadFile(filePath) // #nosec G304 -- filePath sourced from our own symbol index, not user input
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filePath, err)
		}

		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				ct, found := byName[ts.Name.Name]
				if !found {
					continue
				}
				switch t := ts.Type.(type) {
				case *ast.StructType:
					fillFieldList(ct, t.Fields, src, fset, false)
				case *ast.InterfaceType:
					fillFieldList(ct, t.Methods, src, fset, true)
				}
			}
		}
	}
	return nil
}

// fillFieldList walks one struct's field list or one interface's method
// list — both are *ast.FieldList — bucketing each entry into ct's
// Fields/Methods/Embeds. A field/method with no Names is an embed (an
// embedded struct/interface, or an interface embedding another interface);
// isInterface selects which named-entry bucket (Fields vs Methods) to fill.
func fillFieldList(ct *classType, list *ast.FieldList, src []byte, fset *token.FileSet, isInterface bool) {
	if list == nil {
		return
	}
	for _, f := range list.List {
		typeText := sourceSlice(src, fset, f.Type)
		if len(f.Names) == 0 {
			ct.Embeds = append(ct.Embeds, typeText)
			continue
		}
		for _, name := range f.Names {
			m := classMember{Name: name.Name, Type: typeText, Exported: name.IsExported()}
			if isInterface {
				ct.Methods = append(ct.Methods, m)
			} else {
				ct.Fields = append(ct.Fields, m)
			}
		}
	}
}

// fillConcreteMethods adds each struct's real methods (functions whose
// receiver names this type) via query.GetMethodsForType — unlike fields,
// methods ARE fully and correctly indexed already, so this is a plain reuse
// rather than a parse. Interface methods are skipped here; they came from
// the AST walk in parseClassMembers (an interface's method set is its own
// declaration, not a receiver-matched function).
func fillConcreteMethods(db *sql.DB, candidates []*classType) error {
	for _, ct := range candidates {
		if ct.Sym.Kind == cmdKindInterface {
			continue
		}
		methods, err := query.GetMethodsForType(db, ct.Sym.Name, ct.Sym.PkgPath)
		if err != nil {
			return fmt.Errorf("get methods for %s: %w", ct.Sym.Name, err)
		}
		for _, m := range methods {
			ct.Methods = append(ct.Methods, classMember{
				Name: m.Name,
				Type: methodSignatureTail(m.Signature, m.Name),
				// GetMethodsForType's query filters `name GLOB '[A-Z]*'` —
				// every row it returns is already exported.
				Exported: true,
			})
		}
	}
	return nil
}

// methodSignatureTail extracts the "(params) results" tail of a full
// "func (recv) Name(params) results" signature string, so fieldMemberLine
// can prefix it with the method name the same way for both concrete methods
// and interface method signatures (the latter are already just the FuncType
// text, with no name/"func"/receiver prefix to strip).
func methodSignatureTail(sig, name string) string {
	idx := strings.Index(sig, name+"(")
	if idx < 0 {
		return "()"
	}
	return strings.TrimPrefix(sig[idx:], name)
}

// sourceSlice returns the literal source text spanning an AST node's
// position range, read from the same bytes that were parsed. Used instead
// of a go/types formatter since class-map doesn't type-check — the type AS
// WRITTEN is what a comprehension view should show, not its resolved form.
func sourceSlice(src []byte, fset *token.FileSet, n ast.Node) string {
	start := fset.Position(n.Pos()).Offset
	end := fset.Position(n.End()).Offset
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	return string(src[start:end])
}

// rankClassTypes sorts by richness descending (ties broken by name for
// determinism, matching rankSeams' convention) and caps to topN when > 0.
func rankClassTypes(types []*classType, topN int) []*classType {
	sort.Slice(types, func(i, j int) bool {
		if types[i].richness() != types[j].richness() {
			return types[i].richness() > types[j].richness()
		}
		return types[i].Sym.Name < types[j].Sym.Name
	})
	if topN > 0 && len(types) > topN {
		types = types[:topN]
	}
	return types
}

// buildImplementsEdges finds which interfaces each rendered struct
// implements, via query.FindImplementers — the same lookup runDiagramSeams
// uses, without seams' fan-in filter: class-map wants every relationship,
// not just the ones that clear the "real extension point" bar.
//
// Interfaces are pulled into the diagram on demand rather than competing
// against structs in rankClassTypes: an interface typically has far fewer
// fields+methods+embeds than a data-carrying struct, so ranking every
// candidate together would starve interfaces out of the Top-N almost every
// time (observed: 0 of 360 candidates' top 200 by raw richness were
// interfaces). Earning a place by being implemented by a rendered struct is
// both cheaper (no separate interface cap to tune) and more useful (an
// interface with no rendered implementer contributes nothing to THIS
// diagram — `diagram seams` is the dedicated view for interfaces in their
// own right).
func buildImplementsEdges(db *sql.DB, structs, allInterfaces []*classType) (implements map[string][]string, pulled []*classType, err error) {
	structByID := make(map[string]*classType, len(structs))
	for _, s := range structs {
		structByID[s.Sym.ID] = s
	}

	implements = make(map[string][]string)
	pulledSet := make(map[string]*classType)
	for _, iface := range allInterfaces {
		impls, ferr := query.FindImplementers(db, iface.Sym.ID, 1000, 0)
		if ferr != nil {
			return nil, nil, fmt.Errorf("find implementers of %s: %w", iface.Sym.Name, ferr)
		}
		for i := range impls {
			impl := &impls[i]
			if _, ok := structByID[impl.ID]; !ok {
				continue // implementer isn't among the rendered structs
			}
			implements[impl.ID] = append(implements[impl.ID], iface.Sym.ID)
			pulledSet[iface.Sym.ID] = iface
		}
	}

	pulled = make([]*classType, 0, len(pulledSet))
	for _, iface := range pulledSet {
		pulled = append(pulled, iface)
	}
	sort.Slice(pulled, func(i, j int) bool { return pulled[i].Sym.Name < pulled[j].Sym.Name })

	return implements, pulled, nil
}

// classNodeID derives a stable, D2-safe node id from a type's package path
// and name — qualified so two same-named types in different packages never
// collide.
func classNodeID(sym query.SymbolRow) string {
	return "t_" + diagram.SanitizeID(sym.PkgPath+"."+sym.Name)
}

// buildClassMap renders the Claude-summary text and D2 source for a ranked
// class list, following the same emit shape as the other diagram commands.
func buildClassMap(types []*classType, implements map[string][]string, totalScanned int) (summary, d2src string) {
	var b diagram.Builder
	b.Title = "snipe class map · struct/interface relationships"
	b.Direction = diagramDirRight

	var sb strings.Builder
	fmt.Fprintf(&sb, "class map · %d types rendered (of %d struct/interface scanned) · ranked by field+method+embed count\n", len(types), totalScanned)

	ids := make(map[string]string, len(types))
	names := make(map[string]string, len(types))
	for _, t := range types {
		ids[t.Sym.ID] = classNodeID(t.Sym)
		names[t.Sym.ID] = t.Sym.Name
	}

	for _, t := range types {
		id := ids[t.Sym.ID]
		kindLabel := "struct"
		if t.Sym.Kind == cmdKindInterface {
			kindLabel = "interface"
		}
		fmt.Fprintf(&sb, "\n%s %s  %s:%d  fields=%d methods=%d embeds=%d\n",
			kindLabel, t.Sym.Name, t.Sym.FilePathRel, t.Sym.LineStart, len(t.Fields), len(t.Methods), len(t.Embeds))

		var members []string
		for _, e := range t.Embeds {
			members = append(members, "» "+e)
			fmt.Fprintf(&sb, "  embeds  %s\n", e)
		}
		for _, f := range t.Fields {
			members = append(members, fieldMemberLine(f))
			fmt.Fprintf(&sb, "  field   %-24s %s\n", f.Name, f.Type)
		}
		for _, m := range t.Methods {
			members = append(members, fieldMemberLine(m))
			fmt.Fprintf(&sb, "  method  %s%s\n", m.Name, m.Type)
		}
		if ifaceIDs := implements[t.Sym.ID]; len(ifaceIDs) > 0 {
			ifaceNames := make([]string, len(ifaceIDs))
			for i, ifaceID := range ifaceIDs {
				ifaceNames[i] = names[ifaceID]
			}
			fmt.Fprintf(&sb, "  implements  %s\n", strings.Join(ifaceNames, ", "))
		}

		style := map[string]string{}
		if t.Sym.Kind == cmdKindInterface {
			style[diagramFill] = "#ede9fe"
		}
		b.AddClassNode(id, t.Sym.Name, members, style)
	}

	for _, t := range types {
		if t.Sym.Kind == cmdKindInterface {
			continue
		}
		for _, ifaceID := range implements[t.Sym.ID] {
			if ifaceNodeID, ok := ids[ifaceID]; ok {
				b.AddDashedEdge(ids[t.Sym.ID], ifaceNodeID, "implements")
			}
		}
	}

	return sb.String(), b.Render()
}

// fieldMemberLine formats a struct field or interface method as one D2 class
// body line: a visibility glyph (Go's own exported/unexported, not a
// language concept it actually has) followed by "name: type" or
// "name(params) results" for methods (m.Type already includes the parens).
func fieldMemberLine(m classMember) string {
	vis := "-"
	if m.Exported {
		vis = "+"
	}
	if strings.HasPrefix(m.Type, "(") {
		return fmt.Sprintf("%s%s%s", vis, m.Name, m.Type)
	}
	return fmt.Sprintf("%s%s: %s", vis, m.Name, m.Type)
}

// diagramClassMapTopN caps how many ranked types are rendered; set by
// DiagramClassMapCmd.Run (cmd/commands.go).
var diagramClassMapTopN int

// runDiagramClassMap is the Kong entry point for `snipe diagram class-map`.
func runDiagramClassMap() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, _, err := OpenStore(w, "diagram class-map")
	if err != nil {
		return err
	}
	defer s.Close()

	db := s.DB()
	syms, err := findClassCandidates(db)
	if err != nil {
		return err
	}

	all := make([]*classType, len(syms))
	for i := range syms {
		all[i] = &classType{Sym: syms[i]}
	}
	if err := parseClassMembers(all); err != nil {
		return fmt.Errorf("parse class members: %w", err)
	}
	if err := fillConcreteMethods(db, all); err != nil {
		return fmt.Errorf("fill concrete methods: %w", err)
	}

	var structs, interfaces []*classType
	for _, t := range all {
		if t.Sym.Kind == cmdKindInterface {
			interfaces = append(interfaces, t)
		} else {
			structs = append(structs, t)
		}
	}
	rankedStructs := rankClassTypes(structs, diagramClassMapTopN)

	implements, pulledInterfaces, err := buildImplementsEdges(db, rankedStructs, interfaces)
	if err != nil {
		return err
	}
	rendered := append(append([]*classType{}, rankedStructs...), pulledInterfaces...)

	summary, d2src := buildClassMap(rendered, implements, len(all))
	return emitDoc("class-map", summary, d2src)
}
