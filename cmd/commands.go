package cmd

// This file holds the kong command structs and their Run methods. Each Run
// copies the struct's flags into the package-global flag vars (which the
// existing run* functions read) and then calls the existing run* function.
// Positional args are reassembled into the []string the run* functions expect.

// argsOf builds the positional []string a run* function expects from an
// optional single positional value (empty string => no args).
func argsOf(pos string) []string {
	if pos == "" {
		return nil
	}
	return []string{pos}
}

// --- Orient ---

type ContextCmd struct {
	Path string `arg:"" optional:"" help:"Project path (default: current dir)"`

	// context reuses the global --format flag (json/yaml structured output;
	// default claudish text). It cannot redeclare --format because the global
	// flag is embedded and visible on every subcommand.
	Full          bool `help:"Full architecture dump (all components, flows, boundaries)"`
	Orient        bool `help:"Claude-optimized orientation (default)"`
	OutputNug     bool `name:"output-nug" help:"Output as Orca nugget YAML (for save_nug)"`
	Conventions   bool `help:"Detect coding conventions"`
	SchemaVersion bool `name:"schema-version" help:"Print the context output schema version and exit"`
	KeySymbols    int  `name:"key-symbols" default:"15" help:"Cap ranked key symbols in boot output (orient mode)"`
}

func (c *ContextCmd) Run() error {
	// Mirror the prior cobra behavior: context's --format selects structured
	// output (json/yaml) and defaults to claudish text. Read the global flag,
	// treating its "concise" default as unset.
	if flagPassed("format") {
		contextFormat = responseFormat
	} else {
		contextFormat = ""
	}
	contextFull = c.Full
	contextOutputNug = c.OutputNug
	contextConventions = c.Conventions
	contextSchemaVersion = c.SchemaVersion
	contextKeySymbols = c.KeySymbols
	return runContext(argsOf(c.Path))
}

type OrientCmd struct {
	Out string `help:"Output directory (created if missing)"`
}

func (c *OrientCmd) Run() error {
	orientOut = c.Out
	return runOrient()
}

// --- Navigate ---

type DefCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name"`

	At   string `help:"Position to look up (file:line:col)"`
	File string `help:"List all symbols in file"`
	Pkg  string `help:"List exported symbols in package"`
}

func (c *DefCmd) Run() error {
	defAt = c.At
	defFile = c.File
	defPkg = c.Pkg
	return runDef(argsOf(c.Symbol))
}

type RefsCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name"`

	At    string `help:"Position to look up (file:line:col)"`
	Kind  string `help:"Filter by enclosing kind (func, method, etc.)"`
	File  string `help:"Filter references to those in matching file"`
	Pkg   string `help:"Filter references to those in matching package path"`
	Batch bool   `help:"Read symbol names from stdin (one per line), emit JSONL per symbol (shares one index open)"`
}

func (c *RefsCmd) Run() error {
	refsAt = c.At
	refsKind = c.Kind
	refsFile = c.File
	refsPkg = c.Pkg
	refsBatch = c.Batch
	return runRefs(argsOf(c.Symbol))
}

type CallersCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name or id"`
	ID     string `name:"id" help:"Symbol ID to look up"`
}

func (c *CallersCmd) Run() error {
	callersID = c.ID
	return runCallers(argsOf(c.Symbol))
}

type CalleesCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name or id"`
	ID     string `name:"id" help:"Symbol ID to look up"`
}

func (c *CalleesCmd) Run() error {
	calleesID = c.ID
	return runCallees(argsOf(c.Symbol))
}

type ImplCmd struct {
	Interface string `arg:"" optional:"" help:"Interface name"`
	ID        string `name:"id" help:"Interface ID to look up"`
}

func (c *ImplCmd) Run() error {
	implID = c.ID
	return runImpl(argsOf(c.Interface))
}

type TestsCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name or id"`

	Direct bool   `help:"Only show tests that directly call the symbol (1-hop)"`
	At     string `help:"Position to look up (file:line:col)"`
	ID     string `name:"id" help:"Symbol ID to look up"`
}

func (c *TestsCmd) Run() error {
	testsDirect = c.Direct
	testsAt = c.At
	testsID = c.ID
	return runTests(argsOf(c.Symbol))
}

type TriageCmd struct {
	Files []string `arg:"" optional:"" help:"Files to triage (repo-relative or absolute)"`
}

func (c *TriageCmd) Run() error {
	return runTriage(c.Files)
}

type ImpactCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name or id"`

	Direct bool   `help:"1-hop only (skip transitive callers and tests)"`
	At     string `help:"Position to look up (file:line:col)"`
	ID     string `name:"id" help:"Symbol ID to look up"`
}

func (c *ImpactCmd) Run() error {
	impactDirect = c.Direct
	impactAt = c.At
	impactID = c.ID
	return runImpact(argsOf(c.Symbol))
}

// --- Read ---

type ShowCmd struct {
	ID string `arg:"" help:"Symbol ID"`
}

func (c *ShowCmd) Run() error {
	return runShow([]string{c.ID})
}

type PackCmd struct {
	Symbols []string `arg:"" optional:"" help:"Symbol name(s), package, or hex ID(s)"`

	At           string `help:"Position to look up (file:line:col)"`
	RefsLimit    int    `name:"refs-limit" default:"5" help:"Maximum references to return"`
	CallersLimit int    `name:"callers-limit" default:"5" help:"Maximum callers to return"`
	CalleesLimit int    `name:"callees-limit" default:"5" help:"Maximum callees to return"`
}

func (c *PackCmd) Run() error {
	packAt = c.At
	packRefsLimit = c.RefsLimit
	packCallersLimit = c.CallersLimit
	packCalleesLimit = c.CalleesLimit
	return runPack(c.Symbols)
}

type SymCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name"`

	At           string `help:"Position to look up (file:line:col)"`
	RefsLimit    int    `name:"refs-limit" default:"20" help:"Maximum references to return"`
	CallersLimit int    `name:"callers-limit" default:"10" help:"Maximum callers to return"`
	CalleesLimit int    `name:"callees-limit" default:"10" help:"Maximum callees to return"`
}

func (c *SymCmd) Run() error {
	symAt = c.At
	symRefsLimit = c.RefsLimit
	symCallersLimit = c.CallersLimit
	symCalleesLimit = c.CalleesLimit
	return runSym(argsOf(c.Symbol))
}

type ExplainCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name"`

	Mode     string `default:"normal" enum:"brief,normal,deep" help:"Analysis depth: brief, normal, deep"`
	Warnings string `default:"fast" enum:"none,fast,full" help:"Warning level: none, fast, full"`
	At       string `help:"Position to explain (file:line:col)"`
}

func (c *ExplainCmd) Run() error {
	explainMode = c.Mode
	explainWarnings = c.Warnings
	explainAt = c.At
	return runExplain(argsOf(c.Symbol))
}

type PkgCmd struct {
	Name string `arg:"" help:"Package name"`

	Digest bool `help:"Emit one-shot metric digest (exports/ca/ce/instability/lcom4/cyclo_max) as JSON"`
}

func (c *PkgCmd) Run() error {
	pkgDigest = c.Digest
	return runPkg([]string{c.Name})
}

// --- Find ---

type SearchCmd struct {
	Pattern string `arg:"" help:"Search pattern"`

	File string `help:"Glob pattern to filter files (e.g., \"*.go\", \"store.go\")"`
}

func (c *SearchCmd) Run() error {
	searchFile = c.File
	return runSearch([]string{c.Pattern})
}

type LitsCmd struct {
	Value string `arg:"" help:"String literal or env var name"`
}

func (c *LitsCmd) Run() error {
	return runLits([]string{c.Value})
}

type TraceCmd struct {
	Value string `arg:"" help:"String literal value"`
}

func (c *TraceCmd) Run() error {
	return runTrace([]string{c.Value})
}

// --- Graph & Structure ---

type DepsCmd struct {
	Package string `arg:"" optional:"" help:"Package path"`

	Tree bool `help:"Show full dependency graph"`
}

func (c *DepsCmd) Run() error {
	depsTreeFlag = c.Tree
	return runDeps(argsOf(c.Package))
}

type ImportersCmd struct {
	Package string `arg:"" help:"Package path"`
}

func (c *ImportersCmd) Run() error {
	return runImporters([]string{c.Package})
}

type ImportsCmd struct {
	File string `arg:"" help:"File path"`
}

func (c *ImportsCmd) Run() error {
	return runImports([]string{c.File})
}

type TypesCmd struct {
	Type string `arg:"" optional:"" help:"Type name"`

	At string `help:"Position to look up (file:line:col)"`
}

func (c *TypesCmd) Run() error {
	typesAt = c.At
	return runTypes(argsOf(c.Type))
}

type BoundaryCmd struct {
	A string `arg:"" optional:"" name:"pkgset-a" help:"First package set"`
	B string `arg:"" optional:"" name:"pkgset-b" help:"Second package set"`

	Detailed  bool   `help:"List the crossing symbols, not just counts"`
	Direction string `default:"both" enum:"a-to-b,b-to-a,both" help:"Direction filter: a-to-b, b-to-a, both"`
	Layers    string `help:"Layer spec for layered-boundary analysis"`
}

func (c *BoundaryCmd) Run() error {
	boundaryDetailed = c.Detailed
	boundaryDir = c.Direction
	boundaryLayers = c.Layers
	var args []string
	if c.A != "" {
		args = append(args, c.A)
	}
	if c.B != "" {
		args = append(args, c.B)
	}
	return runBoundary(args)
}

type MetricsCmd struct {
	Top   int    `default:"20" help:"Top-N rows to print"`
	Kind  string `default:"pagerank" help:"Metric kind (or comma-separated list for a merged table): pagerank|hub|authority|in_degree|out_degree|eigenvector|betweenness|cycles|topo|ca|ce|coupling|instability|abstractness|distance|lcom4|cyclo|cognitive|churn|usage"`
	Graph string `default:"imports" help:"Graph kind ('imports' or 'calls')"`
	Pkg   string `help:"Filter to a single package (suffix-matches package import path)"`
}

func (c *MetricsCmd) Run() error {
	metricsTopN = c.Top
	metricsKind = c.Kind
	metricsGraph = c.Graph
	metricsPkg = c.Pkg
	return runMetrics()
}

type HotspotsCmd struct {
	Top  int    `default:"20" help:"Top-N files to print"`
	Pkg  string `help:"Filter to paths containing this substring (e.g. internal/store)"`
	File string `help:"Filter to paths containing this substring"`
}

func (c *HotspotsCmd) Run() error {
	return runHotspots(c.Top, c.Pkg, c.File)
}

type RiskCmd struct {
	Base string `arg:"" help:"Base git ref (e.g. main, origin/main, a SHA)"`
	Head string `arg:"" optional:"" default:"HEAD" help:"Head git ref (default HEAD)"`
}

func (c *RiskCmd) Run() error {
	return runRisk(c.Base, c.Head)
}

type VerifyCmd struct {
	Base string `help:"Base git ref for branch-vs-base diff; omit for working-tree vs HEAD"`
}

func (c *VerifyCmd) Run() error {
	return runVerify(c.Base)
}

type PlanCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name or id"`

	Change     string `enum:"signature,behavior,delete" default:"signature" help:"Which worklist to emit: signature (full), behavior (tests only), or delete (must-remove refs)"`
	At         string `help:"Position to look up (file:line:col)"`
	ID         string `name:"id" help:"Symbol ID to look up"`
	MaxCallers int    `default:"50" help:"Cap total call sites emitted (token budget)"`
}

func (c *PlanCmd) Run() error {
	planAt = c.At
	planID = c.ID
	// Clamp negatives to 0 (most restrictive cap). A negative value must never
	// reach planBuildCallSites, where <0 is the internal unlimited sentinel —
	// a --max-callers=-1 typo would otherwise dump every site.
	planMaxCallers = c.MaxCallers
	if planMaxCallers < 0 {
		planMaxCallers = 0
	}
	return runPlan(c.Change, argsOf(c.Symbol))
}

type SensitiveCmd struct{}

func (c *SensitiveCmd) Run() error {
	return runSensitive()
}

type DiagramCmd struct {
	// diagram reuses the global --format flag (d2 default, or svg). It cannot
	// redeclare --format because the global flag is embedded on every command.
	Arch       DiagramArchCmd       `cmd:"" help:"Package import graph trimmed to the top-N by PageRank, grouped by layer. Writes docs/diagrams/arch.md by default (--format d2/svg for stdout)"`
	Flow       DiagramFlowCmd       `cmd:"" help:"Depth-limited call graph from an entry symbol"`
	Lifecycle  DiagramLifecycleCmd  `cmd:"" help:"Render lifecycle CRUD groups for a type as D2"`
	Seams      DiagramSeamsCmd      `cmd:"" help:"Rank pluggable interfaces by implementation count and fan-in. Writes docs/diagrams/seam-map.md by default (--format d2/svg for stdout)"`
	Datastores DiagramDatastoresCmd `cmd:"" name:"datastore-map" help:"Datastore access map: schema + read/write packages per detected store. Writes docs/diagrams/datastore-map.md by default (--format d2/svg for stdout)"`
	System     DiagramSystemCmd     `cmd:"" name:"system-map" help:"Comprehension view: packages collapsed into a handful of subsystems, only cross-subsystem edges shown. Writes docs/diagrams/system-map.md by default (--format d2/svg for stdout)"`
}

// AfterApply maps the global --format into the diagram package var. Unset
// (no --format passed) stays "": emit() treats that the same as explicit
// "d2" (stdout), but emitDoc() (arch; sn-l1kh) treats truly-unset as the
// signal to write docs/diagrams/<name>.md instead — only an explicit
// --format d2 keeps the old stdout dump.
func (c *DiagramCmd) AfterApply() error {
	if flagPassed("format") {
		diagramFormat = responseFormat
	} else {
		diagramFormat = ""
	}
	return nil
}

type DiagramArchCmd struct {
	Top int `default:"20" help:"Top-N packages by PageRank to include"`
}

func (c *DiagramArchCmd) Run() error {
	diagramArchTopN = c.Top
	return runDiagramArch()
}

type DiagramFlowCmd struct {
	Entry string `arg:"" help:"Entry symbol"`

	Depth  int `default:"3" help:"Maximum call depth"`
	Fanout int `default:"6" help:"Maximum callees per node"`
	TopN   int `name:"top-n" default:"0" help:"PageRank trim (0 = no trim)"`
}

func (c *DiagramFlowCmd) Run() error {
	diagramFlowDepth = c.Depth
	diagramFlowFanout = c.Fanout
	diagramFlowTopN = c.TopN
	return runDiagramFlow([]string{c.Entry})
}

type DiagramLifecycleCmd struct {
	Type string `arg:"" help:"Type name"`
}

func (c *DiagramLifecycleCmd) Run() error {
	return runDiagramLifecycle([]string{c.Type})
}

type DiagramSeamsCmd struct {
	Top int `default:"20" help:"Max ranked seams to render (0 = no cap)"`
}

func (c *DiagramSeamsCmd) Run() error {
	diagramSeamsTopN = c.Top
	return runDiagramSeams()
}

type DiagramDatastoresCmd struct{}

func (c *DiagramDatastoresCmd) Run() error {
	return runDiagramDatastores()
}

type DiagramSystemCmd struct {
	Top int `default:"0" help:"Trim to top-N packages by PageRank before grouping (0 = no trim)"`
}

func (c *DiagramSystemCmd) Run() error {
	systemMapTopN = c.Top
	return runDiagramSystem()
}

type LifecycleCmd struct {
	Type string `arg:"" optional:"" help:"Type name"`

	IncludeTests bool   `name:"include-tests" help:"Fold _test.go refs into role buckets (default: separate)"`
	Depth        int    `default:"3" help:"Caller chain depth"`
	Pkg          string `help:"Restrict to a package"`
	At           string `help:"Position to look up (file:line:col)"`
}

func (c *LifecycleCmd) Run() error {
	lifecycleIncludeTests = c.IncludeTests
	lifecycleCallerDepth = c.Depth
	lifecyclePkg = c.Pkg
	lifecycleAt = c.At
	return runLifecycle(argsOf(c.Type))
}

type DeadcodeCmd struct {
	IncludeTests bool   `name:"include-tests" help:"Count refs from _test.go files (default: ignored)"`
	Pkg          string `help:"Filter to a single package (suffix/substring match on pkg_path)"`
}

func (c *DeadcodeCmd) Run() error {
	deadIncludeTests = c.IncludeTests
	deadPkg = c.Pkg
	return runDeadcode()
}

type GuardCmd struct {
	Spec string `arg:"" optional:"" help:"Spec file path"`
}

func (c *GuardCmd) Run() error {
	return runGuard(argsOf(c.Spec))
}

// --- Embeddings ---

type SimCmd struct {
	Query string `arg:"" optional:"" help:"Query text"`

	Threshold     float64 `default:"0.3" help:"Minimum similarity threshold (0-1)"`
	WithinPkg     bool    `name:"within-pkg" help:"Restrict pair scan to symbols in the same package (with --pairs)"`
	Pairs         bool    `help:"Emit near-duplicate symbol pairs (JSONL) instead of running a query"`
	SharedCallees bool    `name:"shared-callees" help:"Include shared-callee counts on each pair (with --pairs)"`
}

func (c *SimCmd) Run() error {
	simThreshold = c.Threshold
	simWithinPkg = c.WithinPkg
	simPairs = c.Pairs
	simSharedCallees = c.SharedCallees
	return runSim(argsOf(c.Query))
}

type EmbedCmd struct {
	Wait bool `help:"Wait for batch to complete"`
	Poll int  `default:"30" help:"Poll interval in seconds (with --wait)"`
}

func (c *EmbedCmd) Run() error {
	embedWait = c.Wait
	embedPollSecs = c.Poll
	return runEmbedStatus()
}

// --- Edit ---

type EditCmd struct {
	Symbol string `arg:"" optional:"" help:"Symbol name"`

	Operation   string `help:"Edit operation: replace_body, replace_full, insert_after, insert_before"`
	NewCode     string `name:"new-code" help:"New code to insert/replace"`
	NewCodeFile string `name:"new-code-file" help:"File containing new code"`
	Apply       bool   `help:"Apply changes (default: preview only)"`
	Batch       bool   `help:"Read batch operations from stdin"`
	At          string `help:"Position to edit (file:line:col)"`
}

func (c *EditCmd) Run() error {
	editOperation = c.Operation
	editNewCode = c.NewCode
	editNewCodeFile = c.NewCodeFile
	editApply = c.Apply
	editBatch = c.Batch
	editAt = c.At
	return runEdit(argsOf(c.Symbol))
}

// --- Index & Health ---

type IndexCmd struct {
	Path string `arg:"" optional:"" help:"Project path (default: current dir)"`

	Embed       bool   `help:"Generate embeddings (deprecated: use --embed-mode)"`
	EmbedMode   string `name:"embed-mode" default:"auto" enum:"auto,batch,realtime,off" help:"Embedding mode: auto, batch, realtime, off"`
	Enrich      bool   `help:"Generate LLM-based symbol purposes (placeholder, not yet wired)"`
	Force       bool   `help:"Force full re-index even if no changes detected"`
	SkipMetrics bool   `name:"skip-metrics" help:"Skip graph metrics (PageRank) computation"`
}

func (c *IndexCmd) Run() error {
	// Legacy --embed defaults to "embeddings on iff credentials are present"
	// (cobra computed this default dynamically). Kong can't express a dynamic
	// default in a struct tag, so honor an explicit --embed but otherwise fall
	// back to credential detection — without this, resolveEmbedMode silently
	// forces embedModeOff and the index ships with zero embeddings.
	switch {
	case flagPassed("embed"):
		withEmbed = c.Embed
	case c.EmbedMode == embedModeOff:
		// --embed-mode=off must do zero keychain execs; the mode short-circuits
		// in resolveEmbedMode anyway, so skip the credentials probe entirely.
		withEmbed = false
	default:
		withEmbed = defaultWithEmbed()
	}
	embedMode = c.EmbedMode
	forceIndex = c.Force
	skipMetrics = c.SkipMetrics
	return runIndex(argsOf(c.Path))
}

type StatusCmd struct{}

func (c *StatusCmd) Run() error {
	return runStatus()
}

type DoctorCmd struct {
	Probe bool `help:"Live-probe external APIs (e.g. Voyage embeddings) to verify keys work, not just that they're present. Makes a network call."`
}

func (c *DoctorCmd) Run() error {
	return runDoctor(c.Probe)
}

// --- Misc ---

type VersionCmd struct {
	JSON bool `name:"json" help:"Output version info as JSON"`
}

func (c *VersionCmd) Run() error {
	versionJSON = c.JSON
	runVersion()
	return nil
}

// --- Hidden helpers ---

type BaselineCmd struct {
	Output string `short:"o" help:"Output file (default: BASELINE.json)"`
	Name   string `help:"Codebase name for the baseline"`
}

func (c *BaselineCmd) Run() error {
	baselineOutput = c.Output
	baselineName = c.Name
	return runBaseline()
}

type CheckCmd struct {
	Baseline         string  `help:"Reference baseline file (default: BASELINE.json)"`
	Threshold        float64 `default:"15.0" help:"Regression threshold percentage"`
	FailOnRegression bool    `name:"fail-on-regression" help:"Exit with error code if regression detected"`
}

func (c *CheckCmd) Run() error {
	checkBaseline = c.Baseline
	checkThreshold = c.Threshold
	checkFailOnReg = c.FailOnRegression
	return runCheck()
}

// HistoryCmd reuses the global --limit flag for the entry count (cobra used a
// local --limit/-n); it cannot redeclare --limit because the global is embedded.
type HistoryCmd struct{}

func (c *HistoryCmd) Run() error {
	historyLimit = limit
	return runHistory()
}

type SchemaCmd struct {
	Type string `arg:"" optional:"" help:"Type name"`
}

func (c *SchemaCmd) Run() error {
	return runSchema(argsOf(c.Type))
}

type WatchCmd struct {
	Debounce int  `default:"500" help:"Debounce time in milliseconds"`
	Verbose  bool `help:"Verbose output"`
}

func (c *WatchCmd) Run() error {
	watchDebounce = c.Debounce
	watchVerbose = c.Verbose
	return runWatch()
}
