package cmd

// CLI is the root kong grammar. Globals is embedded so the persistent flags
// parse in any position; each field tagged cmd:"" is a subcommand whose Run
// method copies its flags into the package-global flag vars and calls the
// existing run* function unchanged.
//
// Help groups follow the order Claude works in: orient, navigate, read, find,
// change, structure, views, write, system. Each help string is one short line
// so compact help fits a screen. The default command (bare "snipe") runs
// status.
type CLI struct {
	Globals

	Context ContextCmd `cmd:"" group:"Orient:" help:"Start here — repo map: entry points, flows, boundaries"`
	Orient  OrientCmd  `cmd:"" group:"Orient:" help:"Write the full orient bundle to a directory"`

	Def     DefCmd     `cmd:"" group:"Navigate:" help:"Where a symbol is defined"`
	Refs    RefsCmd    `cmd:"" group:"Navigate:" help:"Every reference to a symbol"`
	Callers CallersCmd `cmd:"" group:"Navigate:" help:"Functions that call a symbol"`
	Callees CalleesCmd `cmd:"" group:"Navigate:" help:"Functions a symbol calls"`
	Impl    ImplCmd    `cmd:"" group:"Navigate:" help:"Types that implement an interface"`
	Tests   TestsCmd   `cmd:"" group:"Navigate:" help:"Tests that exercise a symbol"`

	// Read: each step adds to the one before (def < sym < pack < explain).
	Show    ShowCmd    `cmd:"" group:"Read:" help:"Expand an id from any result"`
	Sym     SymCmd     `cmd:"" group:"Read:" help:"def + refs, callers, callees"`
	Pack    PackCmd    `cmd:"" group:"Read:" help:"sym + package role and purpose"`
	Explain ExplainCmd `cmd:"" group:"Read:" help:"pack + prose walkthrough and warnings"`
	Pkg     PkgCmd     `cmd:"" group:"Read:" help:"Package overview and exported symbols"`

	Search SearchCmd `cmd:"" group:"Find:" help:"Text search via ripgrep (no index needed)"`
	Lits   LitsCmd   `cmd:"" group:"Find:" help:"Every location of a string literal or env var"`
	Trace  TraceCmd  `cmd:"" group:"Find:" help:"A string literal with its call context"`
	Sim    SimCmd    `cmd:"" group:"Find:" help:"Semantic search (needs embeddings)"`

	// Change: symbol-scoped first, then diff-scoped, then file-scoped.
	Impact    ImpactCmd    `cmd:"" group:"Change:" help:"Blast radius of changing a symbol"`
	Plan      PlanCmd      `cmd:"" group:"Change:" help:"Ordered edit worklist for changing a symbol"`
	Risk      RiskCmd      `cmd:"" group:"Change:" help:"Risk verdict for a diff (base→head)"`
	Verify    VerifyCmd    `cmd:"" group:"Change:" help:"Minimal go test set for a diff"`
	Triage    TriageCmd    `cmd:"" group:"Change:" help:"Facts for a file set: hotspots, package, tests"`
	Sensitive SensitiveCmd `cmd:"" group:"Change:" help:"Files in security zones (auth, crypto, secrets)"`
	Guard     GuardCmd     `cmd:"" group:"Change:" help:"Check architecture rules; exit non-zero on violation"`

	Deps      DepsCmd      `cmd:"" group:"Structure:" help:"Package dependency graph"`
	Importers ImportersCmd `cmd:"" group:"Structure:" help:"Files that import a package"`
	Imports   ImportsCmd   `cmd:"" group:"Structure:" help:"Packages a file imports"`
	Types     TypesCmd     `cmd:"" group:"Structure:" help:"Type relationships"`
	Boundary  BoundaryCmd  `cmd:"" group:"Structure:" help:"Symbols whose refs cross two package sets"`
	Lifecycle LifecycleCmd `cmd:"" group:"Structure:" help:"Functions that create, mutate, read or delete a type"`
	Hotspots  HotspotsCmd  `cmd:"" group:"Structure:" help:"Files ranked by complexity × churn"`
	Metrics   MetricsCmd   `cmd:"" group:"Structure:" help:"Graph metrics: PageRank, coupling, HITS"`
	Deadcode  DeadcodeCmd  `cmd:"" group:"Structure:" help:"Exported symbols with no non-test refs"`
	C4        C4Cmd        `cmd:"" name:"c4" group:"Structure:" help:"C4 facts: containers, datastores, flows"`

	Diagram DiagramCmd `cmd:"" group:"Views:" help:"D2 diagram source for snipe graphs"`
	Report  ReportCmd  `cmd:"" group:"Views:" help:"HTML dashboard for humans"`

	Edit EditCmd `cmd:"" group:"Write:" help:"AST-aware edit; dry-run unless --apply"`

	Index       IndexCmd   `cmd:"" group:"System:" help:"Build or update the index"`
	Status      StatusCmd  `cmd:"" default:"withargs" group:"System:" help:"Index freshness and counts (bare snipe)"`
	Doctor      DoctorCmd  `cmd:"" group:"System:" help:"Diagnose install, config and index"`
	EmbedStatus EmbedCmd   `cmd:"" name:"embed-status" group:"System:" help:"Batch embedding job status"`
	Version     VersionCmd `cmd:"" group:"System:" help:"Print version"`

	// Hidden helpers (kept from cobra; Hidden in help)
	Baseline BaselineCmd `cmd:"" hidden:"" help:"Capture performance baseline for a codebase"`
	Check    CheckCmd    `cmd:"" hidden:"" help:"Check performance against baseline"`
	History  HistoryCmd  `cmd:"" hidden:"" help:"Show performance history over time"`
	Schema   SchemaCmd   `cmd:"" hidden:"" help:"Output JSON Schema for snipe types"`
	Watch    WatchCmd    `cmd:"" hidden:"" help:"Watch for file changes and reindex"`
}
