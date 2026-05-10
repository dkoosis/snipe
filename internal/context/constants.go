package context

// Package path constants for project structure inference.
const (
	pkgInternalStore   = "internal/store"
	pkgInternalQuery   = "internal/query"
	pkgInternalIndex   = "internal/index"
	pkgInternalOutput  = "internal/output"
	pkgInternalConfig  = "internal/config"
	pkgInternalSearch  = "internal/search"
	pkgInternalEmbed   = "internal/embed"
	pkgInternalContext = "internal/context"
	pkgInternalAnalyze = "internal/analyze"
)

// Short package segment names.
const (
	segCmd     = "cmd"
	segIndex   = "index"
	segOutput  = "output"
	segConfig  = "config"
	segSearch  = "search"
	segEmbed   = "embed"
	segContext = "context"
	segTest    = "test"
)

// Purpose description constants reused in package inference maps.
const (
	purposeBenchmarks       = "Benchmarks and baseline capture"
	purposeDataStorage      = "Data storage and persistence"
	purposeQueryExecution   = "Query execution"
	purposeUtilFunctions    = "Utility functions"
	purposeApplicationLogic = "Application logic"
)

// Common file path constants.
const (
	pathCmdRootGo = "cmd/root.go"
)

// Build/tooling marker names.
const (
	toolMage = "mage"
)

// Function name constants used in flow analysis.
const (
	fnRunDef     = "runDef"
	fnRunContext = "runContext"
	fnRunRefs    = "runRefs"
	fnDef        = "def"
)

// Common short package segment names for query/store inference.
const (
	segStore = "store"
	segQuery = "query"
)
