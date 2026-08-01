package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dkoosis/snipe/internal/c4"
	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/datastoremap"
	"github.com/dkoosis/snipe/internal/diagram"
	"github.com/dkoosis/snipe/internal/lifecycle"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// datastoreListCap bounds how many read/write entries the text summary lists
// per store before truncating with a "+N more" marker — a busy store's
// touchpoint list shouldn't blow out the doc's token budget.
const datastoreListCap = 25

// storeHandleNames are common names for the exported type that represents a
// package's persistence handle. When a datastore's package declares one of
// these, lifecycle classification runs against it specifically rather than
// every exported type in the package — the handle type is what callers
// actually construct/pass around to read or write the store, so its refs are
// the real access surface. Falls back to every exported type when none of
// these names match (see pickStoreTypes).
var storeHandleNames = map[string]bool{
	"store":      true,
	"db":         true,
	"database":   true,
	"client":     true,
	"repo":       true,
	"repository": true,
	"conn":       true,
	"connection": true,
	"handle":     true,
}

// pkgType is the minimal symbol shape findPackageTypes needs — deliberately
// narrower than query.SymbolRow since this command only classifies by name.
type pkgType struct {
	ID   string
	Name string
}

// pickStoreTypes narrows a package's exported types to its persistence
// handle(s) by name (case-insensitive) — see storeHandleNames. Deliberately
// does NOT fall back to "every exported type" when nothing matches: c4's
// datastore detection flags any package that imports the bare "database/sql"
// package as a datastore, even when it only takes a *sql.DB parameter rather
// than owning a store (e.g. cmd, or a query-helper package). Without a named
// handle type, there's no principled "this type is the store" anchor for
// lifecycle classification — running it against every exported type in such
// a package (CLI command structs, unrelated DTOs, ...) produces noise, not
// signal. Callers treat an empty result as "no read/write map for this
// package" rather than guessing.
func pickStoreTypes(types []pkgType) []pkgType {
	var matched []pkgType
	for _, t := range types {
		if storeHandleNames[strings.ToLower(t.Name)] {
			matched = append(matched, t)
		}
	}
	return matched
}

// runDiagramDatastores builds the datastore access map: for each detected
// SQL store, its schema (internal/context.DetectDBSchemas) plus a read-list
// and write-list of packages/functions (internal/c4 datastore detection,
// direction from internal/lifecycle role classification). Written via
// emitDoc to docs/diagrams/datastore-map.md by default, same pattern as
// `diagram arch`/`diagram lifecycle`.
func runDiagramDatastores() error {
	w := output.NewWriter(os.Stdout, GetOutputFormat())
	s, dir, err := OpenStore(w, "diagram datastore-map")
	if err != nil {
		return err
	}
	defer s.Close()

	db := s.DB()
	modulePath := query.DetectModulePath(db)

	facts, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		return fmt.Errorf("build c4 facts: %w", err)
	}
	datastores := datastoremap.GroupByPackage(datastoremap.FilterTestPackages(facts.Datastores))

	schemas := context.DetectDBSchemas(dir)

	stores, err := buildStores(db, schemas, datastores, modulePath)
	if err != nil {
		return err
	}

	summary := renderDatastoreSummary(stores)
	d2src := renderDatastoreDiagram(stores).Render()

	return emitDoc("datastore-map", summary, d2src)
}

// buildStores correlates each detected schema to its owning package (when
// resolvable), classifies that package's persistence-handle type(s) via
// internal/lifecycle, and buckets the results into read/write lists.
// Datastore packages with no matching schema still get a Store entry (schema
// unknown, access map populated) so a driver-detected store never vanishes
// silently just because DetectDBSchemas couldn't find its DDL.
func buildStores(db *sql.DB, schemas []context.DBSchema, datastores []c4.Datastore, modulePath string) ([]datastoremap.Store, error) {
	matchedPkgs := make(map[string]bool, len(schemas))
	stores := make([]datastoremap.Store, 0, len(schemas)+len(datastores))

	for _, schema := range schemas {
		st := datastoremap.Store{
			Name:         schema.Name,
			SchemaSource: schema.Source,
			DDL:          schema.DDL,
		}
		if pkg, ok := datastoremap.MatchSchema(schema, datastores, modulePath); ok {
			st.Package = pkg
			matchedPkgs[pkg] = true
			classifications, err := classifyPackage(db, pkg)
			if err != nil {
				return nil, err
			}
			st.Reads, st.Writes, st.Unclear = datastoremap.BucketRoles(classifications)
		}
		stores = append(stores, st)
	}

	// Datastore packages that no schema claimed still get an entry — a real
	// touchpoint c4 found, just without known DDL. But c4's "SQL" fallback
	// fires on any package that merely takes a *sql.DB parameter (e.g. a
	// query-helper package), not just packages that own a store — if
	// classifyPackage found no persistence-handle type at all (see
	// pickStoreTypes), there's nothing to show, so skip rather than emit an
	// empty, noisy entry.
	for _, d := range datastores {
		if matchedPkgs[d.Package] {
			continue
		}
		classifications, err := classifyPackage(db, d.Package)
		if err != nil {
			return nil, err
		}
		reads, writes, unclear := datastoremap.BucketRoles(classifications)
		if len(reads) == 0 && len(writes) == 0 && unclear == 0 {
			continue
		}
		stores = append(stores, datastoremap.Store{
			Name: d.Name, Package: d.Package,
			Reads: reads, Writes: writes, Unclear: unclear,
		})
	}

	return stores, nil
}

// classifyPackage finds pkg's persistence-handle type(s) and runs
// internal/lifecycle classification against each, merging the results.
func classifyPackage(db *sql.DB, pkg string) ([]lifecycle.Classification, error) {
	types, err := findPackageTypes(db, pkg)
	if err != nil {
		return nil, err
	}
	if len(types) == 0 {
		return nil, nil
	}

	var out []lifecycle.Classification
	for _, t := range pickStoreTypes(types) {
		refRows, err := query.FindRefs(db, t.ID, 10000, 0)
		if err != nil {
			return nil, fmt.Errorf("find refs for %s: %w", t.Name, err)
		}
		refs := lifecycle.FromRefRows(refRows)
		out = append(out, lifecycle.Classify(t.Name, refs)...)
	}
	return out, nil
}

// findPackageTypes returns every exported struct/type declared directly in
// pkg (excluding _test.go files) — candidates for pickStoreTypes.
func findPackageTypes(db *sql.DB, pkg string) ([]pkgType, error) {
	rows, err := db.Query(`
		SELECT id, name
		FROM symbols
		WHERE pkg_path = ?
		  AND kind IN (?, ?)
		  AND length(name) > 0
		  AND substr(name, 1, 1) = upper(substr(name, 1, 1))
		  AND file_path_rel NOT LIKE '%_test.go'
		ORDER BY line_start
	`, pkg, cmdKindType, cmdKindStruct)
	if err != nil {
		return nil, fmt.Errorf("query package types: %w", err)
	}
	defer rows.Close()

	var out []pkgType
	for rows.Next() {
		var t pkgType
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("scan package type: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// renderDatastoreSummary renders the Claude-facing plain-text section:
// schema DDL then read/write lists, per store.
func renderDatastoreSummary(stores []datastoremap.Store) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diagram datastore-map · %d store(s)\n", len(stores))

	for _, st := range stores {
		label := st.Name
		if st.Package != "" {
			label = fmt.Sprintf("%s (%s)", st.Name, st.Package)
		}
		fmt.Fprintf(&b, "\n## %s\n", label)

		if st.SchemaSource != "" {
			fmt.Fprintf(&b, "schema: %s\n", st.SchemaSource)
			if st.DDL != "" {
				b.WriteString(st.DDL)
				if !strings.HasSuffix(st.DDL, "\n") {
					b.WriteString("\n")
				}
			}
		} else {
			b.WriteString("schema: (not detected)\n")
		}

		if st.Package == "" {
			b.WriteString("(no owning package resolved — read/write map unavailable)\n")
			continue
		}

		fmt.Fprintf(&b, "\nreads (%d):\n", len(st.Reads))
		writeEntryList(&b, st.Reads)
		fmt.Fprintf(&b, "writes (%d):\n", len(st.Writes))
		writeEntryList(&b, st.Writes)
		if st.Unclear > 0 {
			fmt.Fprintf(&b, "unclear: %d classification(s) neither read nor write\n", st.Unclear)
		}
	}

	return b.String()
}

func writeEntryList(b *strings.Builder, entries []datastoremap.Entry) {
	shown := entries
	truncated := 0
	if len(shown) > datastoreListCap {
		truncated = len(shown) - datastoreListCap
		shown = shown[:datastoreListCap]
	}
	for _, e := range shown {
		name := e.Func
		if name == "" {
			name = "(file scope)"
		}
		fmt.Fprintf(b, "  %s  %s:%d  [%s]\n", name, e.File, e.Line, e.Role)
	}
	if truncated > 0 {
		fmt.Fprintf(b, "  ... +%d more\n", truncated)
	}
}

// datastorePerRoleCap bounds how many function nodes each read/write
// container draws in the D2 diagram — the text summary above lists more
// (datastoreListCap); the diagram stays legible at a smaller cap.
const datastorePerRoleCap = 8

// renderDatastoreDiagram builds a D2 diagram: one cylinder per store, with
// "reads"/"writes" containers of function nodes, edges pointing store->read
// and write->store (matching cmd/diagram.go's runDiagramLifecycle
// convention: writes flow into the data, reads flow out of it).
func renderDatastoreDiagram(stores []datastoremap.Store) *diagram.Builder {
	var b diagram.Builder
	b.Title = "snipe datastore access map"
	b.Direction = "right"

	for _, st := range stores {
		storeID := diagram.SanitizeID(st.Name + "_" + st.Package)
		storeNode := "store_" + storeID
		b.AddNode(storeNode, st.Name, "cylinder", map[string]string{diagramFill: diagramColorWarn, "bold": diagramTrue})

		if st.Package == "" {
			continue
		}

		// Containers are siblings of storeNode, not its children: dagre
		// (the D2 layout engine emitDoc shells out to) rejects an edge from a
		// container to its own descendant, and storeNode -> read-fn / write-fn
		// -> storeNode edges are exactly that shape if the containers nest
		// under storeNode.
		if len(st.Reads) > 0 {
			container := "reads_" + storeID
			b.AddContainer(container, st.Name+" reads", map[string]string{diagramFill: "#dbeafe"})
			addRoleNodes(&b, container, st.Reads, func(fnNode string) {
				b.AddEdge(storeNode, fnNode, "read", nil)
			})
		}
		if len(st.Writes) > 0 {
			container := "writes_" + storeID
			b.AddContainer(container, st.Name+" writes", map[string]string{diagramFill: "#fef9c3"})
			addRoleNodes(&b, container, st.Writes, func(fnNode string) {
				b.AddEdge(fnNode, storeNode, "write", nil)
			})
		}
	}

	return &b
}

func addRoleNodes(b *diagram.Builder, container string, entries []datastoremap.Entry, edge func(fnNode string)) {
	sorted := make([]datastoremap.Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		return sorted[i].Line < sorted[j].Line
	})

	shown := sorted
	truncated := 0
	if len(shown) > datastorePerRoleCap {
		truncated = len(shown) - datastorePerRoleCap
		shown = shown[:datastorePerRoleCap]
	}
	for _, e := range shown {
		name := e.Func
		if name == "" {
			name = "(file scope)"
		}
		id := container + "." + diagram.SanitizeID(name+fmt.Sprintf("%d", e.Line))
		label := fmt.Sprintf("%s · %s:%d", name, e.File, e.Line)
		b.AddNode(id, label, "", nil)
		edge(id)
	}
	if truncated > 0 {
		id := container + "._more"
		b.AddNode(id, fmt.Sprintf("+%d more", truncated), "", map[string]string{"font-color": "#6b7280"})
	}
}
