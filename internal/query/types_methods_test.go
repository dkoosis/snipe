package query_test

import (
	"database/sql"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/dkoosis/snipe/internal/query"
)

const symbolsSchema = `CREATE TABLE symbols (id TEXT, name TEXT, kind TEXT, signature TEXT, receiver TEXT, file_path_rel TEXT, line_start INTEGER, doc TEXT);`

func TestGetMethodsForType_ReturnsExportedMethodsByReceiver_When_QueryingType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		seedSQL         []string
		closeDB         bool
		input           string
		want            []query.MethodInfo
		wantErrContains string
	}{
		{
			name:            "error on closed database",
			closeDB:         true,
			input:           "Widget",
			wantErrContains: "sql: database is closed",
		},
		{
			name:    "boundary empty typename yields no results",
			seedSQL: []string{`INSERT INTO symbols VALUES ('1','Do','method','func()','(Widget)','a.go',10,'');`},
			input:   "",
			want:    nil,
		},
		{
			name: "happy path includes value and pointer exported methods only",
			seedSQL: []string{
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
		},
		{
			name:    "boundary zero values in nullable columns become empty strings",
			seedSQL: []string{`INSERT INTO symbols (id,name,kind,receiver,file_path_rel,line_start) VALUES ('9','Zeta','method','(Widget)','z.go',90);`},
			input:   "Widget",
			want:    []query.MethodInfo{{ID: "9", Name: "Zeta", Signature: "", Receiver: "(Widget)", File: "z.go", Line: 90, Doc: ""}},
		},
	}

	for _, tc := range tests {
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
		})
	}
}

func openSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(symbolsSchema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
