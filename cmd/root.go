package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/dkoosis/snipe/internal/config"
	ctxpkg "github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/telemetry"
	"github.com/dkoosis/snipe/internal/util"
)

var (
	// Global flags
	limit         int
	offset        int
	contextLines  int
	noBody        bool
	noSiblings    bool
	signatureOnly bool
	maxTokens     int
	timeout       time.Duration

	// response_format mode: concise, detailed, or summary
	responseFormat string

	// KG integration
	withKGHints bool

	// Opt-in suggestion output (off by default in Claude mode)
	showSuggestions bool

	// Selection mode for multi-result commands
	selectMode string

	// Caller passthrough for correlation
	caller string

	// Internal: auto-compact when piped
	autoCompact bool

	// cmdCtx is the context for the current command (with timeout and signal handling)
	cmdCtx    context.Context
	cmdCancel context.CancelFunc
)

// rootHelp is the long description shown in `snipe --help`.
const rootHelp = `snipe: Go code navigation for Claude.

  snipe index              Build index (run first)
  snipe def ProcessOrder   Jump to definition
  snipe refs ProcessOrder  Find all references
  snipe callers Handler    Who calls this?
  snipe pack ProcessOrder  Everything about a symbol
  snipe search "TODO"      Text search (no index needed)

  snipe doctor             Check index health
  snipe context            Claude-optimized orientation
  snipe context --full     Full architecture dump

Pick the right overview command:
  context   start here — project orientation (read first)
  pack      deep dive on one symbol or package
  sym       symbol-only (def+refs+callers+callees), no package context
  explain   structured function walkthrough (LLM consumption)

Typical flow:  context → pack <symbol> → callers/callees → tests <symbol>
IDs chain:     every result's 'id' field is valid input to the next command
Index:         all commands except 'search' need a built index — run 'snipe index' first
Writes:        only 'edit' modifies files; all other commands are read-only`

// Globals holds the persistent flags shared by every command. It is embedded
// into the root CLI struct so the flags parse in any position, and its
// AfterApply hook copies the parsed values into the package-global flag vars
// the run* functions read, then runs the former cobra PersistentPreRunE logic.
type Globals struct {
	Limit         int           `help:"Cap results returned (applied after --select)" default:"10"`
	Offset        int           `help:"Pagination offset" default:"0"`
	Context       int           `help:"Context lines around match" default:"2"`
	NoBody        bool          `name:"no-body" help:"Exclude function body"`
	NoSiblings    bool          `name:"no-siblings" help:"Exclude sibling declarations"`
	SignatureOnly bool          `name:"signature-only" help:"Return only signature (no body, no context)"`
	MaxTokens     int           `name:"max-tokens" help:"Token budget (0 = unlimited)" default:"0"`
	Format        string        `help:"concise (LLM default) | detailed | summary | json (stable, for tooling) | human (TTY)" default:"concise"`
	KGHints       bool          `name:"kg-hints" help:"Include Orca KG hints"`
	NoSuggestions bool          `name:"no-suggestions" help:"Suppress next-step suggestions in Claude output"`
	Timeout       time.Duration `help:"Timeout for command (e.g., 30s, 5m)"`
	Select        string        `help:"Pick top candidates by score: all, best, top3, top5 (applied before --limit)" default:"all" enum:"all,best,top3,top5"`
	// Reserved for orca telemetry — hidden until persistToolCall is wired.
	Caller    string `help:"Caller identifier (e.g., 'orca')" hidden:""`
	RequestID string `name:"request-id" help:"Request correlation ID" hidden:""`
}

// AfterApply copies parsed globals into the package vars and runs the
// per-invocation setup (context/signal handling, config defaults, output
// wiring) that cobra's PersistentPreRunE used to perform.
func (g *Globals) AfterApply() error {
	limit = g.Limit
	offset = g.Offset
	contextLines = g.Context
	noBody = g.NoBody
	noSiblings = g.NoSiblings
	signatureOnly = g.SignatureOnly
	maxTokens = g.MaxTokens
	responseFormat = g.Format
	withKGHints = g.KGHints
	showSuggestions = !g.NoSuggestions
	timeout = g.Timeout
	selectMode = g.Select
	caller = g.Caller

	// Set up context with signal handling for graceful cancellation
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	cmdCancel = cancel

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\nInterrupted, cleaning up...")
			cmdCancel()
		case <-ctx.Done():
		}
		signal.Stop(sigCh)
	}()

	cmdCtx = ctx

	// Load config and apply defaults if flags weren't explicitly set
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Apply config defaults only if flags weren't explicitly set
	if !flagPassed("limit") && cfg.Limit > 0 {
		limit = cfg.Limit
	}
	if !flagPassed("context") && cfg.ContextLines > 0 {
		contextLines = cfg.ContextLines
	}

	// Auto-compact when output is piped (not a TTY)
	autoCompact = true

	// Wire suggestion opt-in into output package
	output.SetShowSuggestions(showSuggestions)

	return nil
}

// AfterRun cancels the command context to release the signal goroutine. It is
// the kong analogue of cobra's PersistentPostRun.
func (g *Globals) AfterRun() error {
	if cmdCancel != nil {
		cmdCancel()
	}
	return nil
}

// flagPassed reports whether the user explicitly passed --name on the command
// line (matching "--name" or "--name=value"). It replicates cobra's
// Flags().Changed() for the handful of places that need "only override if the
// user didn't set it" semantics.
func flagPassed(name string) bool {
	pfx := "--" + name
	for _, a := range os.Args[1:] {
		if a == pfx || strings.HasPrefix(a, pfx+"=") {
			return true
		}
	}
	return false
}

// knownSubcommandNames is the set of recognized subcommand names, used by the
// bare-symbol fallback to decide whether "snipe X" should be rewritten to
// "snipe sym X". Derived from the kong grammar at Parse time.
var knownSubcommandNames = map[string]bool{}

// isKnownSubcommandOrFlag checks if arg is a subcommand or flag.
func isKnownSubcommandOrFlag(arg string) bool {
	// Flags start with -
	if len(arg) > 0 && arg[0] == '-' {
		return true
	}
	return knownSubcommandNames[arg]
}

// globalValueFlags are the persistent flags that take a separate value
// argument (the "--flag value" form), needed so indexOfHelpCommand can skip
// the value when scanning for the first positional.
var globalValueFlags = map[string]bool{
	"--limit": true, "--offset": true, "--context": true, "--max-tokens": true,
	"--format": true, "--timeout": true, "--select": true,
	"--caller": true, "--request-id": true,
}

// indexOfHelpCommand returns the index of the first positional token that
// equals "help" (skipping leading global flags and any values they consume),
// or -1 if the first positional is not "help".
func indexOfHelpCommand(args []string) int {
	i := 0
	for i < len(args) {
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			// "--flag value" consumes the next token; "--flag=value" does not.
			if globalValueFlags[a] {
				i += 2
			} else {
				i++
			}
			continue
		}
		// First positional token.
		if a == "help" {
			return i
		}
		return -1
	}
	return -1
}

// Execute parses os.Args with kong and runs the selected command. A bare
// symbol ("snipe Store") with no matching subcommand is rewritten to
// "snipe sym Store" to preserve the cobra one-command-just-works behavior.
func Execute() {
	cli := &CLI{}
	parser, err := kong.New(cli,
		kong.Name("snipe"),
		kong.Description(rootHelp),
		kong.UsageOnError(),
		kong.ConfigureHelp(kong.HelpOptions{Compact: false}),
		// Own the exit code (AXI #6): --help still exits 0, but a flag/usage
		// error exits a deliberate 2 instead of kong's default. Run and
		// error-envelope exits (1) are handled below, bypassing kong.
		kong.Exit(func(code int) {
			if code != 0 {
				code = 2
			}
			os.Exit(code)
		}),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Build the known-subcommand set from the kong grammar so new commands
	// never need manual registration — a missed registration would silently
	// route "snipe X" to the bare-symbol fallback.
	for _, n := range parser.Model.Children {
		if n.Name != "" {
			knownSubcommandNames[n.Name] = true
		}
	}
	knownSubcommandNames["help"] = true

	args := os.Args[1:]

	// "snipe help [cmd...]" -> "snipe [cmd...] --help" (cobra had a help
	// subcommand; kong uses the --help flag). The "help" token may follow
	// leading global flags (e.g. orca prepends --format json).
	if i := indexOfHelpCommand(args); i >= 0 {
		rest := make([]string, 0, len(args))
		rest = append(rest, args[:i]...)
		rest = append(rest, args[i+1:]...)
		rest = append(rest, "--help")
		args = rest
	}

	// Bare-symbol fallback: "snipe Store" -> "snipe sym Store".
	if len(args) > 0 && !isKnownSubcommandOrFlag(args[0]) {
		args = append([]string{cmdNameSym}, args...)
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		// Prints usage (UsageOnError) then exits 2 via the kong.Exit hook above.
		parser.FatalIfErrorf(err)
	}

	if err := ctx.Run(); err != nil {
		// An internal Go error from a command (rare — most command-level
		// problems are emitted via WriteError and return nil). Exit 1 directly,
		// bypassing kong so the usage-error hook can't remap it to 2.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The command ran, but may have emitted an error envelope (e.g. symbol not
	// found) and returned nil. Reflect that in the exit code so agents chaining
	// commands can detect failure (AXI #6).
	if output.ProcessErrored() {
		os.Exit(1)
	}
}

// GetContext returns the command context (with timeout and signal handling).
// Returns context.Background() if called before AfterApply.
func GetContext() context.Context {
	if cmdCtx == nil {
		return context.Background()
	}
	return cmdCtx
}

// ResponseFormat represents output format modes for go_symbol parity.
type ResponseFormat string

const (
	// FormatDefault uses command-specific defaults.
	FormatDefault ResponseFormat = ""
	// FormatConcise strips bodies, minimal metadata.
	FormatConcise ResponseFormat = "concise"
	// FormatDetailed includes full metadata and all hints.
	FormatDetailed ResponseFormat = "detailed"
	// FormatSummary aggregates results by file (counts only).
	FormatSummary ResponseFormat = "summary"
	// FormatHuman renders one-line plain text for direct CLI consumption.
	FormatHuman ResponseFormat = "human"
)

// GetOutputConfig returns the current output configuration.
// Returns: compact, limit, offset, contextLines, withBody, withSiblings
func GetOutputConfig() (compact bool, lim int, off int, ctx int, body bool, siblings bool) {
	// Apply signature-only override
	if signatureOnly {
		return autoCompact, limit, offset, 0, false, false
	}
	return autoCompact, limit, offset, contextLines, !noBody, !noSiblings
}

// GetResponseFormat returns the response format mode.
func GetResponseFormat() ResponseFormat {
	return ResponseFormat(responseFormat)
}

// GetOutputFormat returns the output format for the Writer.
// "json" = full JSON envelope, "human" = one-line plain text for CLI use,
// default = Claude-optimized text.
func GetOutputFormat() output.OutputFormat {
	switch responseFormat {
	case "json":
		return output.OutputJSON
	case "human":
		return output.OutputHuman
	}
	return output.OutputClaude
}

// ApplyFormatOverrides adjusts output config based on --format flag.
// Returns (withBody, withSiblings, contextLines) based on format mode.
func ApplyFormatOverrides(format ResponseFormat, baseBody, baseSiblings bool, baseContext int) (bool, bool, int) {
	switch format {
	case FormatConcise:
		// Concise: no body, no siblings, minimal context
		return false, false, 0
	case FormatDetailed:
		// Detailed: everything enabled, full context
		return true, true, baseContext
	case FormatSummary:
		// Summary: no body needed (just counts)
		return false, false, 0
	case FormatDefault:
		// Default: use base values
		return baseBody, baseSiblings, baseContext
	case FormatHuman:
		// Human: one-line output; body/siblings/context are noise.
		return false, false, 0
	}
	// Unreachable, but satisfies exhaustive check
	return baseBody, baseSiblings, baseContext
}

// GetWithKGHints returns whether KG hints should be included.
func GetWithKGHints() bool {
	return withKGHints
}

// GetMaxTokens returns the max-tokens flag value (0 = unlimited)
func GetMaxTokens() int {
	return maxTokens
}

// uniqueStrings removes duplicates from a string slice, preserving order.
func uniqueStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

// ApplySelection truncates results based on the --select flag.
// Should be called after ScoreAndSort.
func ApplySelection(results []output.Result) []output.Result {
	switch selectMode {
	case selectBest:
		if len(results) > 1 {
			return results[:1]
		}
	case selectTop3:
		if len(results) > 3 {
			return results[:3]
		}
	case selectTop5:
		if len(results) > 5 {
			return results[:5]
		}
	}
	// "all" or unrecognized: return everything
	return results
}

// pickSelectedSymbol returns the highest-scored candidate when --select is
// set to a narrowing mode (best/top3/top5). It is the ambiguity fallback
// path: instead of erroring when several symbols share a name, take the
// top-scoring one. Returns ok=false when --select is "all" (or empty),
// preserving the explicit ambiguity error for users who did not opt in.
func pickSelectedSymbol(symbols []query.SymbolRow, name string) (query.SymbolRow, bool) {
	switch selectMode {
	case selectBest, selectTop3, selectTop5:
	default:
		return query.SymbolRow{}, false
	}
	results := make([]output.Result, len(symbols))
	for i := range symbols {
		results[i] = symbols[i].ToResult()
	}
	output.ScoreAndSort(results, name)
	topID := results[0].ID
	for i := range symbols {
		s := &symbols[i]
		if s.ID == topID {
			return *s, true
		}
	}
	return symbols[0], true
}

// OpenStore opens the index for query commands.
// Returns the store, project root, and any error.
func OpenStore(w *output.Writer, cmdName string) (*store.Store, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		if w != nil {
			_ = w.WriteError(cmdName, &output.Error{
				Code:    output.ErrInternal,
				Message: "failed to get working directory: " + err.Error(),
			})
		}
		return nil, "", err
	}

	// Resolve to git/go.mod root — index always lives at project root (D3)
	root := util.FindProjectRoot(cwd)
	if root != "" {
		telemetry.SetRoot(root)
		telemetry.SetCaller(caller)
		telemetry.SetSessionKey(telemetry.ResolveSessionKey(root))
	}
	if root == "" {
		root = cwd // fallback: not in a git/go.mod repo, use CWD
	}

	dbPath := store.DefaultIndexPath(root)
	dbExists := store.Exists(dbPath)

	// Mismatch detection: old index built at wrong location by previous bug
	if !dbExists && root != cwd {
		cwdPath := store.DefaultIndexPath(cwd)
		if store.Exists(cwdPath) {
			if w != nil {
				_ = w.WriteError(cmdName, &output.Error{
					Code:    output.ErrIndexMismatch,
					Message: fmt.Sprintf("index found at %s but project root is %s — run: snipe index", cwd, root),
				})
			}
			return nil, root, fmt.Errorf("index root mismatch")
		}
	}

	// Check if indexing is in progress
	if store.IsIndexing(dbPath) {
		if w != nil {
			_ = w.WriteError(cmdName, output.NewIndexInProgressError())
		}
		return nil, root, fmt.Errorf("indexing in progress")
	}

	// Check for missing index
	if !dbExists {
		if w != nil {
			_ = w.WriteError(cmdName, output.NewMissingIndexError())
		}
		return nil, root, fmt.Errorf("index missing")
	}

	s, err := store.Open(dbPath)
	if err != nil {
		if w != nil {
			_ = w.WriteError(cmdName, &output.Error{
				Code:    output.ErrInternal,
				Message: "failed to open index: " + err.Error(),
			})
		}
		return nil, root, err
	}

	// Small drift heals inline so queries answer from current code; the
	// subprocess takes its own lock, so reopen to see the new data.
	if maybeSelfHeal(s, root) {
		_ = s.Close()
		s, err = store.Open(dbPath)
		if err != nil {
			return nil, root, err
		}
	}

	// Stamp the index-global self-assessment marker once: every query command
	// routes through OpenStore, so this covers all of them uniformly (snipe-ffj).
	if w != nil {
		w.SetEmbedMissing(query.EmbedMissing(s.DB()))
	}

	return s, root, nil
}

// recordSessionQuery records a symbol query in the session for active work tracking.
// This is a best-effort operation - errors are silently ignored to not affect command execution.
func recordSessionQuery(projectRoot, symbol, file string, line int, kind, command string) {
	session, err := ctxpkg.LoadSession(projectRoot)
	if err != nil {
		return
	}
	session.RecordQuery(symbol, file, line, kind, command)
	_ = ctxpkg.SaveSession(session)
}
