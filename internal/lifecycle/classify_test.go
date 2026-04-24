package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The canonical test: one ref → one function → expected (role, rule id).
// Rule id is matched as a substring of Signal so the test stays stable if
// signal formatting shifts.
type caseT struct {
	name     string
	typeName string
	refs     []Ref
	wantRole Role
	wantRule string   // substring expected in Signal
	wantMix  []Role   // optional secondary roles
}

func TestClassify_Rules(t *testing.T) {
	cases := []caseT{
		// R1: evidence-based Delete beats name-based Create.
		{
			name:     "R1 beats naming: RemoveDuplicates returning slice is Read not Delete",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn1",
				EnclosingName:      "RemoveDuplicates",
				EnclosingSignature: "func RemoveDuplicates(nugs []*Nug) []*Nug",
				FileRel:            "nugs/dedupe.go",
				Line:               10,
				Snippet:            "seen := map[string]bool{}",
			}},
			// No Create snippet, no delete evidence, name-Delete fires (R4).
			// This is the known false-positive we accept for name-only Delete.
			wantRole: RoleDelete,
			wantRule: "[R4 name]",
		},
		{
			name:     "R1: store.Delete(nug) beats everything",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn2",
				EnclosingName:      "purgeOld",
				EnclosingSignature: "func purgeOld(s *Store, n *Nug) error",
				FileRel:            "nugs/purge.go",
				Line:               12,
				Snippet:            "return s.Delete(n)",
			}},
			wantRole: RoleDelete,
			wantRule: "[R1 delete-call]",
		},

		// R2: snippet Create beats name Delete.
		{
			name:     "R2 beats R4: DeleteAndReplace that allocates is Create",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn3",
				EnclosingName:      "DeleteAndReplace",
				EnclosingSignature: "func DeleteAndReplace(id string) *Nug",
				FileRel:            "nugs/swap.go",
				Line:               7,
				Snippet:            "return &Nug{ID: id}",
			}},
			wantRole: RoleCreate,
			wantRule: "[R2 snippet]",
			wantMix:  []Role{RoleDelete}, // R4 also fires → Mixed
		},
		{
			name:     "R2: new(T)",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn4",
				EnclosingName:      "bootstrap",
				EnclosingSignature: "func bootstrap() any",
				FileRel:            "nugs/boot.go",
				Line:               3,
				Snippet:            "n := new(Nug)",
			}},
			wantRole: RoleCreate,
			wantRule: "[R2 new()]",
		},
		{
			name:     "R2: make([]T)",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn5",
				EnclosingName:      "preallocate",
				EnclosingSignature: "func preallocate(n int) []Nug",
				FileRel:            "nugs/pre.go",
				Line:               2,
				Snippet:            "return make([]Nug, 0, n)",
			}},
			wantRole: RoleCreate,
			wantRule: "[R2 make()]",
		},

		// Word-boundary: NugStore must not match T=Nug patterns.
		{
			name:     "word boundary: new(NugStore) does NOT match Create for T=Nug",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn6",
				EnclosingName:      "wrap",
				EnclosingSignature: "func wrap() *NugStore",
				FileRel:            "nugs/wrap.go",
				Line:               2,
				Snippet:            "return new(NugStore)",
			}},
			// Nothing matches: no snippet Create on Nug, name is `wrap`,
			// no delete signal → R7 default Read.
			wantRole: RoleRead,
			wantRule: "[R7 default]",
		},
		{
			name:     "word boundary: &NugList{} does NOT match Create for T=Nug",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn7",
				EnclosingName:      "collect",
				EnclosingSignature: "func collect() *NugList",
				FileRel:            "nugs/collect.go",
				Line:               5,
				Snippet:            "return &NugList{}",
			}},
			wantRole: RoleRead,
			wantRule: "[R7 default]",
		},

		// R3: name + sig.
		{
			name:     "R3: NewNug returns *Nug",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn8",
				EnclosingName:      "NewNug",
				EnclosingSignature: "func NewNug(id string) *Nug",
				FileRel:            "nugs/build.go",
				Line:               3,
				Snippet:            "n := &thing{}",
			}},
			// Note: snippet does not match Nug{ → R3 fires.
			wantRole: RoleCreate,
			wantRule: "[R3 name+sig]",
		},

		// R4: name-only Delete.
		{
			name:     "R4: DeleteNug with no snippet evidence",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn9",
				EnclosingName:      "DeleteNug",
				EnclosingSignature: "func DeleteNug(id string) error",
				FileRel:            "nugs/del.go",
				Line:               3,
				Snippet:            "return errors.New(\"nug not found\")",
			}},
			wantRole: RoleDelete,
			wantRule: "[R4 name]",
		},

		// CamelCase boundary: CreateRemovalJob must NOT trip R4 Delete.
		{
			name:     "camelCase boundary: CreateRemovalJob does NOT match Delete verbs",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn10",
				EnclosingName:      "CreateRemovalJob",
				EnclosingSignature: "func CreateRemovalJob(n *Nug) *Job",
				FileRel:            "nugs/job.go",
				Line:               4,
				Snippet:            "return &Job{}",
			}},
			// Create name matches R3 but signature does not return *Nug.
			// No other rule fires → R7 Read.
			wantRole: RoleRead,
			wantRule: "[R7 default]",
		},

		// R5: mutating name + *T param.
		{
			name:     "R5: UpdateNug takes *Nug",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn11",
				EnclosingName:      "UpdateNug",
				EnclosingSignature: "func UpdateNug(n *Nug, fields map[string]any) error",
				FileRel:            "nugs/update.go",
				Line:               5,
				Snippet:            "n.Updated = time.Now()",
			}},
			wantRole: RoleMutate,
			wantRule: "[R5 name+sig]",
		},
		{
			name:     "R5: PrintNug takes *Nug but wrong verb → Read",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn12",
				EnclosingName:      "PrintNug",
				EnclosingSignature: "func PrintNug(n *Nug)",
				FileRel:            "nugs/print.go",
				Line:               4,
				Snippet:            "fmt.Println(n.ID)",
			}},
			wantRole: RoleRead,
			wantRule: "[R7 default]",
		},

		// R6: file-scope init.
		{
			name:     "R6: package-level var with struct literal is Create",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "", // file-scope
				EnclosingName:      "",
				EnclosingSignature: "",
				FileRel:            "nugs/defaults.go",
				Line:               1,
				Snippet:            "var defaultNug = &Nug{ID: \"default\"}",
			}},
			wantRole: RoleCreate,
			wantRule: "[R6 file-scope]",
		},

		// R8: file-scope without Create signal.
		{
			name:     "R8: file-scope type declaration",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "",
				FileRel:            "nugs/nug.go",
				Line:               10,
				Snippet:            "type Nug struct {",
			}},
			wantRole: RoleUnknown,
			wantRule: "[R8 no-enclosing]",
		},

		// Regression: function signature line must not trigger R2 Create.
		// `func F(...) *T {` has `T {` but the brace opens the body.
		{
			name:     "signature line does NOT fire R2",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn_sig",
				EnclosingName:      "FindNug",
				EnclosingSignature: "func FindNug(id string) *Nug",
				FileRel:            "nugs/find.go",
				Line:               10,
				Snippet:            "func FindNug(id string) *Nug {",
			}},
			wantRole: RoleRead,
			wantRule: "[R7 default]",
		},

		// Mixed: UpsertNug both creates and mutates.
		{
			name:     "mixed: UpsertNug with literal AND mutator name",
			typeName: "Nug",
			refs: []Ref{{
				EnclosingID:        "fn13",
				EnclosingName:      "UpsertNug",
				EnclosingSignature: "func UpsertNug(s *Store, n *Nug) error",
				FileRel:            "nugs/upsert.go",
				Line:               6,
				Snippet:            "s.cache[id] = &Nug{ID: id}",
			}},
			wantRole: RoleCreate,
			wantRule: "[R2 snippet]",
			wantMix:  []Role{RoleMutate},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.typeName, tc.refs)
			assert.Len(t, got, 1)
			if len(got) != 1 {
				return
			}
			assert.Equal(t, tc.wantRole, got[0].Role, "role")
			assert.Contains(t, got[0].Signal, tc.wantRule, "rule id")
			assert.ElementsMatch(t, tc.wantMix, got[0].Mixed, "mixed")
		})
	}
}

func TestClassify_AggregatesRefsPerFunction(t *testing.T) {
	// Two refs in the same enclosing fn: one construction + one field assignment.
	// Classification runs once, primary = R2 Create, Mixed = none (R5 gated on name).
	refs := []Ref{
		{
			EnclosingID:        "fn1",
			EnclosingName:      "loadOrBuild",
			EnclosingSignature: "func loadOrBuild() *Nug",
			FileRel:            "nugs/load.go",
			Line:               5,
			Snippet:            "n := &Nug{}",
		},
		{
			EnclosingID:        "fn1",
			EnclosingName:      "loadOrBuild",
			EnclosingSignature: "func loadOrBuild() *Nug",
			FileRel:            "nugs/load.go",
			Line:               6,
			Snippet:            "return n",
		},
	}
	got := Classify("Nug", refs)
	assert.Len(t, got, 1)
	assert.Equal(t, RoleCreate, got[0].Role)
}

func TestClassify_TestFileDetection(t *testing.T) {
	refs := []Ref{
		{
			EnclosingID:        "tfn",
			EnclosingName:      "TestNugCreate",
			EnclosingSignature: "func TestNugCreate(t *testing.T)",
			FileRel:            "nugs/nug_test.go",
			Line:               5,
			Snippet:            "n := &Nug{ID: \"x\"}",
		},
	}
	got := Classify("Nug", refs)
	assert.Len(t, got, 1)
	assert.True(t, got[0].IsTestFile)
	assert.Equal(t, RoleCreate, got[0].Role)
}

func TestIsTestOrGeneratedFile(t *testing.T) {
	cases := map[string]bool{
		"pkg/foo_test.go":           true,
		"pkg/foo_gen.go":            true,
		"pkg/zz_generated_deep.go":  true,
		"pkg/foo.go":                false,
		"zz_generated_root.go":      true,
		"pkg/generated_manual.go":   false, // only zz_generated_ prefix counts
	}
	for path, want := range cases {
		assert.Equal(t, want, isTestOrGeneratedFile(path), path)
	}
}
