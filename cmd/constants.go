// Package cmd defines shared string constants used across cobra command
// annotations, flag names, and common error messages.
package cmd

// Cobra command annotation category labels.
// Buckets organize commands by task in --help output.
const (
	categoryNavigate = "navigate" // def refs callers callees impl tests impact
	categoryRead     = "read"     // show pack sym explain pkg
	categoryFind     = "find"     // search lits trace
	categoryGraph    = "graph"    // deps importers imports types boundary metrics diagram lifecycle deadcode
	categoryOrient   = "orient"   // context orient
	categoryEmbed    = "embed"    // sim embed-status
	categoryEdit     = "edit"     // edit
	categoryIndex    = "index"    // index status doctor
)

// Common command names (used as annotations and in routing).
const (
	cmdNameDef         = "def"
	cmdNameRefs        = "refs"
	cmdNameCallers     = "callers"
	cmdNameCallees     = "callees"
	cmdNameImpact      = "impact"
	cmdNameImpl        = "impl"
	cmdNameEdit        = "edit"
	cmdNameDoctor      = "doctor"
	cmdNameSim         = "sim"
	cmdNameMetrics     = "metrics"
	cmdNameSym         = "sym"
	cmdNameSearch      = "search"
	cmdNameShow        = "show"
	cmdNameTests       = "tests"
	cmdNameTypes       = "types"
	cmdNamePack        = "pack"
	cmdNamePkg         = "pkg"
	cmdNameImports     = "imports"
	cmdNameImporters   = "importers"
	cmdNameExplain     = "explain"
	cmdNameStatus      = "status"
	cmdNameIndex       = "index"
	cmdNameEmbedStatus = "embed-status"
)

// Common flag/parameter names.
const (
	flagSymbol  = "symbol"
	flagFile    = "file"
	flagPkg     = "pkg"
	flagPackage = "package"
	flagRef     = "ref"
)

// Common selection/format values.
const (
	selectBest = "best"
	selectTop3 = "top3"
	selectTop5 = "top5"
)

// Common kind/type literal values used in cmd output.
const (
	cmdKindFunc      = "func"
	cmdKindType      = "type"
	cmdKindError     = "error"
	cmdKindGraph     = "graph"
	cmdKindCyclo     = "cyclo"
	cmdKindCycles    = "cycles"
	cmdKindCoupling  = "coupling"
	cmdKindInterface = "interface"
	cmdKindStruct    = "struct"
	cmdKindTopo      = "topo"
	cmdKindDistance  = "distance"
)

// Common JSON / map keys produced by cmd output.
const (
	jsonKeyKind = "kind"
)

// Diagram styling constants.
const (
	diagramFill      = "fill"
	diagramColorWarn = "#fde68a"
	diagramTrue      = "true"
)

// Common error message strings.
const (
	errProvideSymbolOrAt = "provide a symbol name or --at position"
)
