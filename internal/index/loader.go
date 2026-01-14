package index

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

// LoadConfig configures how packages are loaded
type LoadConfig struct {
	// Dir is the directory to load packages from
	Dir string
	// Patterns are the package patterns to load (e.g., "./...")
	Patterns []string
	// Exclude patterns to skip (e.g., "vendor", "testdata")
	Exclude []string
	// Tests includes test files
	Tests bool
}

// LoadResult contains the loaded packages and metadata
type LoadResult struct {
	Packages []*packages.Package
	Fset     *token.FileSet
	Errors   []error
}

// DefaultExclude returns the default exclude patterns
func DefaultExclude() []string {
	return []string{"vendor", "node_modules", "testdata", ".git"}
}

// Load loads Go packages from the specified directory
func Load(cfg LoadConfig) (*LoadResult, error) {
	if cfg.Dir == "" {
		cfg.Dir = "."
	}
	if len(cfg.Patterns) == 0 {
		cfg.Patterns = []string{"./..."}
	}
	if cfg.Exclude == nil {
		cfg.Exclude = DefaultExclude()
	}

	// Resolve absolute path
	absDir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolve directory: %w", err)
	}

	fset := token.NewFileSet()

	// Configure packages.Load
	loadCfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports,
		Dir:   absDir,
		Fset:  fset,
		Tests: cfg.Tests,
		// BuildFlags can be added here if needed
	}

	pkgs, err := packages.Load(loadCfg, cfg.Patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	// Collect errors from packages
	var errs []error
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			errs = append(errs, e)
		}
	}

	// Filter out excluded paths
	filtered := filterPackages(pkgs, cfg.Exclude)

	return &LoadResult{
		Packages: filtered,
		Fset:     fset,
		Errors:   errs,
	}, nil
}

// filterPackages removes packages matching exclude patterns
func filterPackages(pkgs []*packages.Package, exclude []string) []*packages.Package {
	if len(exclude) == 0 {
		return pkgs
	}

	var result []*packages.Package
	for _, pkg := range pkgs {
		excluded := false
		for _, pattern := range exclude {
			// Simple substring match for now
			if matchesExclude(pkg.PkgPath, pattern) {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, pkg)
		}
	}
	return result
}

func matchesExclude(pkgPath, pattern string) bool {
	// Split by "/" to get path components (package paths use forward slashes)
	for _, component := range strings.Split(pkgPath, "/") {
		if component == pattern {
			return true
		}
	}
	return false
}

// WalkFiles walks all Go files in the loaded packages
func WalkFiles(result *LoadResult, fn func(pkg *packages.Package, file *ast.File, path string) error) error {
	for _, pkg := range result.Packages {
		for i, file := range pkg.Syntax {
			if i >= len(pkg.GoFiles) {
				continue
			}
			path := pkg.GoFiles[i]
			if err := fn(pkg, file, path); err != nil {
				return err
			}
		}
	}
	return nil
}
