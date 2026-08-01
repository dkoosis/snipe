package context

import "testing"

func findTable(tables []Table, name string) (Table, bool) {
	for _, t := range tables {
		if t.Name == name {
			return t, true
		}
	}
	return Table{}, false
}

func findColumn(cols []Column, name string) (Column, bool) {
	for _, c := range cols {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

func TestParseSchema_Basic(t *testing.T) {
	ddl := `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email VARCHAR(255) NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE posts (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	body TEXT,
	FOREIGN KEY (user_id) REFERENCES users(id)
);
`
	tables := ParseSchema(ddl)
	if len(tables) != 2 {
		t.Fatalf("want 2 tables, got %d: %+v", len(tables), tables)
	}

	users, ok := findTable(tables, "users")
	if !ok {
		t.Fatalf("missing users table: %+v", tables)
	}
	if len(users.Columns) != 3 {
		t.Fatalf("users: want 3 columns, got %d: %+v", len(users.Columns), users.Columns)
	}
	id, ok := findColumn(users.Columns, "id")
	if !ok || id.Type != "INTEGER" {
		t.Errorf("users.id: want type INTEGER, got %+v (found=%v)", id, ok)
	}
	email, ok := findColumn(users.Columns, "email")
	if !ok || email.Type != "VARCHAR(255)" {
		t.Errorf("users.email: want type VARCHAR(255), got %+v (found=%v)", email, ok)
	}

	posts, ok := findTable(tables, "posts")
	if !ok {
		t.Fatalf("missing posts table: %+v", tables)
	}
	// FOREIGN KEY line is a table-level constraint, not a column.
	if len(posts.Columns) != 3 {
		t.Errorf("posts: want 3 columns (FOREIGN KEY excluded), got %d: %+v", len(posts.Columns), posts.Columns)
	}
	if _, ok := findColumn(posts.Columns, "user_id"); !ok {
		t.Errorf("posts: missing user_id column: %+v", posts.Columns)
	}
}

func TestParseSchema_CompositePrimaryKeyExcluded(t *testing.T) {
	ddl := `
CREATE TABLE call_graph (
	caller_id TEXT NOT NULL,
	callee_id TEXT NOT NULL,
	line INT NOT NULL,
	col INT NOT NULL,
	PRIMARY KEY (caller_id, callee_id, line, col),
	FOREIGN KEY (caller_id) REFERENCES symbols(id),
	FOREIGN KEY (callee_id) REFERENCES symbols(id)
);
`
	tables := ParseSchema(ddl)
	if len(tables) != 1 {
		t.Fatalf("want 1 table, got %d: %+v", len(tables), tables)
	}
	cg := tables[0]
	if len(cg.Columns) != 4 {
		t.Fatalf("want 4 real columns (constraints excluded), got %d: %+v", len(cg.Columns), cg.Columns)
	}
	for _, want := range []string{"caller_id", "callee_id", "line", "col"} {
		if _, ok := findColumn(cg.Columns, want); !ok {
			t.Errorf("missing column %q: %+v", want, cg.Columns)
		}
	}
}

func TestParseSchema_IgnoresCreateIndex(t *testing.T) {
	ddl := `
CREATE TABLE widgets (id TEXT PRIMARY KEY, name TEXT);
CREATE INDEX idx_widgets_name ON widgets(name);
CREATE UNIQUE INDEX idx_widgets_id ON widgets(id);
`
	tables := ParseSchema(ddl)
	if len(tables) != 1 {
		t.Fatalf("want 1 table (indexes ignored), got %d: %+v", len(tables), tables)
	}
	if tables[0].Name != "widgets" {
		t.Errorf("want widgets, got %q", tables[0].Name)
	}
}

func TestParseSchema_QuotedIdentifiers(t *testing.T) {
	ddl := "CREATE TABLE `orders` (`id` TEXT PRIMARY KEY, `total` DECIMAL(10,2));"
	tables := ParseSchema(ddl)
	if len(tables) != 1 {
		t.Fatalf("want 1 table, got %d: %+v", len(tables), tables)
	}
	if tables[0].Name != "orders" {
		t.Errorf("want unquoted name 'orders', got %q", tables[0].Name)
	}
	total, ok := findColumn(tables[0].Columns, "total")
	if !ok || total.Type != "DECIMAL(10,2)" {
		t.Errorf("want total DECIMAL(10,2), got %+v (found=%v)", total, ok)
	}
}

func TestParseSchema_Empty(t *testing.T) {
	if got := ParseSchema(""); len(got) != 0 {
		t.Errorf("want 0 tables for empty ddl, got %d", len(got))
	}
	if got := ParseSchema("-- just a comment, no DDL"); len(got) != 0 {
		t.Errorf("want 0 tables for DDL-free input, got %d", len(got))
	}
}

// TestParseSchema_RealSchema exercises ParseSchema end-to-end against
// DetectDBSchemas' output for a real fixture (rather than a hand-written
// DDL string), confirming the two functions compose the way a future
// "table X: columns a, b, c" renderer would use them.
func TestParseSchema_RealSchema(t *testing.T) {
	schemas := DetectDBSchemas("testdata/dbschema/embedded-go")
	if len(schemas) != 1 {
		t.Fatalf("want 1 detected schema, got %d", len(schemas))
	}
	tables := ParseSchema(schemas[0].DDL)
	symbols, ok := findTable(tables, "symbols")
	if !ok {
		t.Fatalf("missing symbols table from parsed DDL: %+v", tables)
	}
	if len(symbols.Columns) != 3 {
		t.Fatalf("symbols: want 3 columns, got %d: %+v", len(symbols.Columns), symbols.Columns)
	}
	id, ok := findColumn(symbols.Columns, "id")
	if !ok || id.Type != "TEXT" {
		t.Errorf("symbols.id: want TEXT, got %+v (found=%v)", id, ok)
	}
}

// TestParseSchema_SnipeItself parses snipe's own store schema (via
// DetectDBSchemas on the real repo) and checks the symbols table's columns
// come out structured, with FOREIGN KEY / composite PRIMARY KEY lines
// correctly excluded from the column list.
func TestParseSchema_SnipeItself(t *testing.T) {
	schemas := DetectDBSchemas("../..")
	var storeSchema *DBSchema
	for i := range schemas {
		if schemas[i].Name == "store" {
			storeSchema = &schemas[i]
			break
		}
	}
	if storeSchema == nil {
		t.Fatalf("expected a 'store' schema from snipe's own repo; got: %v", sources(schemas))
	}

	tables := ParseSchema(storeSchema.DDL)
	symbols, ok := findTable(tables, "symbols")
	if !ok {
		t.Fatalf("missing symbols table: %+v", tables)
	}
	for _, want := range []string{"id", "name", "kind", "file_path"} {
		if _, ok := findColumn(symbols.Columns, want); !ok {
			t.Errorf("symbols: missing column %q: %+v", want, symbols.Columns)
		}
	}

	callGraph, ok := findTable(tables, "call_graph")
	if !ok {
		t.Fatalf("missing call_graph table: %+v", tables)
	}
	for _, want := range []string{"caller_id", "callee_id", "file_path", "line", "col"} {
		if _, ok := findColumn(callGraph.Columns, want); !ok {
			t.Errorf("call_graph: missing column %q: %+v", want, callGraph.Columns)
		}
	}
	// The composite PRIMARY KEY and two FOREIGN KEY lines are table-level
	// constraints, not columns — exactly 5 real columns.
	if len(callGraph.Columns) != 5 {
		t.Errorf("call_graph: want 5 columns (constraints excluded), got %d: %+v", len(callGraph.Columns), callGraph.Columns)
	}
}
