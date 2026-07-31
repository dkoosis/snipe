// Package cmd defines shared string constants used across command names, flag
// names, and common error messages.
package cmd

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
	cmdNameHotspots    = "hotspots"
	cmdNameRisk        = "risk"
	cmdNameSym         = "sym"
	cmdNameSearch      = "search"
	cmdNameShow        = "show"
	cmdNameTests       = "tests"
	cmdNameTriage      = "triage"
	cmdNameTypes       = "types"
	cmdNameVerify      = "verify"
	cmdNamePlan        = "plan"
	cmdNamePack        = "pack"
	cmdNamePkg         = "pkg"
	cmdNameImports     = "imports"
	cmdNameImporters   = "importers"
	cmdNameExplain     = "explain"
	cmdNameStatus      = "status"
	cmdNameIndex       = "index"
	cmdNameEmbedStatus = "embed-status"
	cmdNameC4          = "c4"
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
	cmdKindCognitive = "cognitive"
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
