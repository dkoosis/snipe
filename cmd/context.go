package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/store"
	"github.com/dkoosis/snipe/internal/util"
)

var (
	contextFormat        string
	contextFull          bool
	contextOutputNug     bool
	contextConventions   bool
	contextSchemaVersion bool
)

func runContext(args []string) error {
	if contextSchemaVersion {
		fmt.Println(context.SchemaVersion)
		return nil
	}

	// Determine directory
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Find project root
	projectRoot := util.FindProjectRoot(absDir)
	if projectRoot == "" {
		return fmt.Errorf("not in a git repository")
	}

	// Open index
	indexPath := store.DefaultIndexPath(projectRoot)
	if !store.Exists(indexPath) {
		return fmt.Errorf("index not found at %s\nRun 'snipe index' first", indexPath)
	}

	s, err := store.Open(indexPath)
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer s.Close()

	// Use the repo_root stored at index time — this ensures path stripping
	// matches the paths in the DB (avoids macOS case-sensitivity mismatches).
	if storedRoot, err := s.GetMeta("repo_root"); err == nil && storedRoot != "" {
		projectRoot = storedRoot
	}

	// Generate context
	cfg := context.GenerateConfig{
		RepoRoot: projectRoot,
		DB:       s.DB(),
		Full:     contextFull,
	}

	// --full and --conventions have no claudish text formatter; they default
	// to yaml so bare invocations work (the empty format used to be rejected).
	structuredFormat := contextFormat
	if !flagPassed("format") {
		structuredFormat = "yaml"
	}

	if contextConventions {
		conv := context.DetectConventions(s.DB(), projectRoot)
		return outputContext(conv, structuredFormat)
	}

	if contextFull {
		// Full architecture dump (--full)
		ctx, err := context.Generate(cfg)
		if err != nil {
			return fmt.Errorf("generate context: %w", err)
		}

		if contextOutputNug {
			nugs := ctx.ToNuggets()
			return outputNuggets(nugs)
		}

		return outputContext(ctx, structuredFormat)
	}

	// Default: Claude-optimized orientation (bare or --orient)
	orientCtx, err := context.GenerateBoot(cfg)
	if err != nil {
		return fmt.Errorf("generate orientation context: %w", err)
	}
	orientCtx.IndexState = string(query.CheckIndexState(s.DB(), projectRoot, Version))

	if contextOutputNug {
		nugs := orientCtx.ToNuggets()
		return outputNuggets(nugs)
	}

	// Claudish text is the default for orient mode (D1: Claude is the consumer).
	// --format json/yaml overrides for orca/toolchain integration.
	if !flagPassed("format") {
		fmt.Print(context.FormatText(orientCtx))
		return nil
	}

	return outputContext(orientCtx, contextFormat)
}

func outputContext(output interface{}, format string) error {
	switch format {
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		if err := enc.Encode(output); err != nil {
			return fmt.Errorf("encode yaml: %w", err)
		}
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(output); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format: %s (use json or yaml)", format)
	}
	return nil
}

func outputNuggets(nugs []context.Nugget) error {
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	for _, nug := range nugs {
		if err := enc.Encode(nug); err != nil {
			return fmt.Errorf("encode nug yaml: %w", err)
		}
		fmt.Println("---")
	}
	return nil
}
