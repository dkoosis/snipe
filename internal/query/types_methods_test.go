package query_test

import (
	"database/sql"
	"testing"

	"github.com/dkoosis/snipe/internal/query"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestGetMethodsForType_ReturnsExportedMethodsByReceiver_When_QueryingType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seedSQL []string
		closeDB bool
		input   string
		want    []query.MethodInfo
		wantErrContains string
		inspect func(*testing.T, []query.MethodInfo)
	}{
		{
			name: "error on closed database without schema",
			closeDB: true,
			input:   "Widget",
			wantErrContains: "sql: database is closed",
		},
		{
			name: "error on closed database handle",
			seedSQL: []string{
				`CREATE TABLE symbols (id TEXT, name TEXT, kind TEXT, signature TEXT, receiver TEXT, file_path_rel TEXT, line_start INTEGER, doc TEXT);`,
			},
			closeDB: true,
			input:   "Widget",
			wantErrContains: "sql: database is closed",
		},
		{
			name: "boundary empty typename yields no results",
			seedSQL: []string{
				`CREATE TABLE symbols (id TEXT, name TEXT, kind TEXT, signature TEXT, receiver TEXT, file_path_rel TEXT, line_start INTEGER, doc TEXT);`,
				`INSERT INTO symbols VALUES ('1','Do','method','func()','(Widget)','a.go',10,'');`,
			},
			input: "",
			want:  nil,
			inspect: func(t *testing.T, got []query.MethodInfo) {
				t.Helper()
				require.Empty(t, got)
			},
		},
		{
			name: "happy path includes value and pointer exported methods only",
			seedSQL: []string{
				`CREATE TABLE symbols (id TEXT, name TEXT, kind TEXT, signature TEXT, receiver TEXT, file_path_rel TEXT, line_start INTEGER, doc TEXT);`,
				`INSERT INTO symbols VALUES ('1','Alpha','method','func()','(Widget)','a.go',10,'doc A');`,
				`INSERT INTO symbols VALUES ('2','Beta','method','func(int)','(*Widget)','a.go',20,'doc B');`,
				`INSERT INTO symbols VALUES ('3','gamma','method','func()','(Widget)','a.go',30,'private');`,
				`INSERT INTO symbols VALUES ('4','Nope','func','func()','','a.go',40,'not method');`,
				`INSERT INTO symbols VALUES ('5','Other','method','func()','(Other)','b.go',50,'other type');`,
			},
			input: "Widget",
			want: []query.MethodInfo{
				{ID: "1", Name: "Alpha", Signature: "func()", Receiver: "(Widget)", File: "a.go", Line: 10, Doc: "doc A"},
				{ID: "2", Name: "Beta", Signature: "func(int)", Receiver: "(*Widget)", File: "a.go", Line: 20, Doc: "doc B"},
			},
			inspect: func(t *testing.T, got []query.MethodInfo) {
				t.Helper()
				for _, m := range got {
					require.NotEmpty(t, m.ID)
					require.Contains(t, []string{"(Widget)", "(*Widget)"}, m.Receiver)
					require.Regexp(t, "^[A-Z]", m.Name)
				}
			},
		},
		{
			name: "boundary zero values in nullable columns become empty strings",
			seedSQL: []string{
				`CREATE TABLE symbols (id TEXT, name TEXT, kind TEXT, signature TEXT, receiver TEXT, file_path_rel TEXT, line_start INTEGER, doc TEXT);`,
				`INSERT INTO symbols (id,name,kind,receiver,file_path_rel,line_start) VALUES ('9','Zeta','method','(Widget)','z.go',90);`,
			},
			input: "Widget",
			want: []query.MethodInfo{{ID: "9", Name: "Zeta", Signature: "", Receiver: "(Widget)", File: "z.go", Line: 90, Doc: ""}},
			inspect: func(t *testing.T, got []query.MethodInfo) {
				t.Helper()
				require.Len(t, got, 1)
				require.Empty(t, got[0].Signature)
				require.Empty(t, got[0].Doc)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openSQLiteDB(t)
			for _, stmt := range tc.seedSQL {
				_, err := db.Exec(stmt)
				require.NoError(t, err)
			}
			if tc.closeDB {
				require.NoError(t, db.Close())
			}

			got, err := query.GetMethodsForType(db, tc.input, "ignored")

			if tc.wantErrContains != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.wantErrContains)
				return
			}
			require.NoError(t, err)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("diff (-want +got):\n%s", diff)
			}

			if tc.inspect != nil {
				tc.inspect(t, got)
			}
		})
	}
}

func openSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
