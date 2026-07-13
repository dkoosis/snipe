package cmd

// CLI is the root kong grammar. Globals is embedded so the persistent flags
// parse in any position; each field tagged cmd:"" is a subcommand whose Run
// method copies its flags into the package-global flag vars and calls the
// existing run* function unchanged.
//
// Help groups mirror the former cobra categories. The default command (bare
// "snipe") runs status.
type CLI struct {
	Globals

	// Orient
	Context ContextCmd `cmd:"" group:"Orient Project:" help:"Start here — Claude-optimized project orientation"`
	Orient  OrientCmd  `cmd:"" group:"Orient Project:" help:"Write a full orient bundle (context-full, deps-tree, all metrics, manifest) into a directory"`

	// Navigate
	Def     DefCmd     `cmd:"" group:"Navigate Symbol:" help:"Jump to symbol definition"`
	Refs    RefsCmd    `cmd:"" group:"Navigate Symbol:" help:"Find all references to a symbol"`
	Callers CallersCmd `cmd:"" group:"Navigate Symbol:" help:"Find functions that call a symbol"`
	Callees CalleesCmd `cmd:"" group:"Navigate Symbol:" help:"Find functions that a symbol calls"`
	Impl    ImplCmd    `cmd:"" group:"Navigate Symbol:" help:"Find types implementing an interface"`
	Tests   TestsCmd   `cmd:"" group:"Navigate Symbol:" help:"Find tests that exercise a symbol"`
	Impact  ImpactCmd  `cmd:"" group:"Navigate Symbol:" help:"Show blast radius for changing a symbol"`

	// Read
	Show    ShowCmd    `cmd:"" group:"Read Symbol or Package:" help:"Show symbol details by ID"`
	Pack    PackCmd    `cmd:"" group:"Read Symbol or Package:" help:"Deep dive on one symbol or package (def+refs+callers+callees+role+purpose)"`
	Sym     SymCmd     `cmd:"" group:"Read Symbol or Package:" help:"Symbol-only — def+refs+callers+callees, no package context (lighter than pack)"`
	Explain ExplainCmd `cmd:"" group:"Read Symbol or Package:" help:"Structured function walkthrough — purpose, mechanism, callers, warnings"`
	Pkg     PkgCmd     `cmd:"" group:"Read Symbol or Package:" help:"Show package overview with exported symbols"`

	// Find
	Search SearchCmd `cmd:"" group:"Find by Text:" help:"Text search via ripgrep"`
	Lits   LitsCmd   `cmd:"" group:"Find by Text:" help:"Find all locations of a string literal or env var name"`
	Trace  TraceCmd  `cmd:"" group:"Find by Text:" help:"Trace a string literal through its call context"`

	// Graph & Structure
	Deps      DepsCmd      `cmd:"" group:"Graph & Structure:" help:"Show dependency topology for a package or the full project"`
	Importers ImportersCmd `cmd:"" group:"Graph & Structure:" help:"Find files that import a package"`
	Imports   ImportsCmd   `cmd:"" group:"Graph & Structure:" help:"Show packages imported by a file"`
	Types     TypesCmd     `cmd:"" group:"Graph & Structure:" help:"Show type relationships"`
	Boundary  BoundaryCmd  `cmd:"" group:"Graph & Structure:" help:"Show symbols whose refs cross between two package sets"`
	Metrics   MetricsCmd   `cmd:"" group:"Graph & Structure:" help:"Show graph metrics (PageRank, coupling, HITS, etc.) over the import or call graph"`
	Hotspots  HotspotsCmd  `cmd:"" group:"Graph & Structure:" help:"Rank files by complexity × git change-frequency (Tornhill hotspot model)"`
	Risk      RiskCmd      `cmd:"" group:"Graph & Structure:" help:"Assess a diff's risk (base→head): code-graph verdict (roles, centrality, blast, churn)"`
	Verify    VerifyCmd    `cmd:"" group:"Graph & Structure:" help:"Map a diff to the minimal go test set covering changed symbols (structural, not a gate)"`
	Sensitive SensitiveCmd `cmd:"" group:"Graph & Structure:" help:"List files in security-sensitive zones (auth/crypto/migration/secret/payment)"`
	Diagram   DiagramCmd   `cmd:"" group:"Graph & Structure:" help:"Render snipe graphs as D2 diagram source"`
	Lifecycle LifecycleCmd `cmd:"" group:"Graph & Structure:" help:"Trace every function creating, mutating, reading, or deleting a type"`
	Deadcode  DeadcodeCmd  `cmd:"" group:"Graph & Structure:" help:"Report exported symbols with zero non-test references"`
	Guard     GuardCmd     `cmd:"" group:"Graph & Structure:" help:"Assert architecture boundary rules; exit non-zero on violation"`

	// Embeddings
	Sim         SimCmd   `cmd:"" group:"Embeddings:" help:"Semantic similarity search"`
	EmbedStatus EmbedCmd `cmd:"" name:"embed-status" group:"Embeddings:" help:"Check status of batch embedding job"`

	// Edit
	Edit EditCmd `cmd:"" group:"Edit (modifies files):" help:"AST-aware code editing — modifies files (dry-run unless --apply)"`

	// Index & Health
	Index  IndexCmd  `cmd:"" group:"Index & Health:" help:"Build or update the code index"`
	Status StatusCmd `cmd:"" default:"withargs" group:"Index & Health:" help:"Show index status and statistics"`
	Doctor DoctorCmd `cmd:"" group:"Index & Health:" help:"Check snipe installation and configuration"`

	// Misc / utility
	Version VersionCmd `cmd:"" help:"Print version information"`

	// Hidden helpers (kept from cobra; Hidden in help)
	Baseline BaselineCmd `cmd:"" hidden:"" help:"Capture performance baseline for a codebase"`
	Check    CheckCmd    `cmd:"" hidden:"" help:"Check performance against baseline"`
	History  HistoryCmd  `cmd:"" hidden:"" help:"Show performance history over time"`
	Schema   SchemaCmd   `cmd:"" hidden:"" help:"Output JSON Schema for snipe types"`
	Watch    WatchCmd    `cmd:"" hidden:"" help:"Watch for file changes and reindex"`
}
