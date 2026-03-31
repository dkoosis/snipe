package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMakefileTargets(t *testing.T) {
	content := `.PHONY: build test lint
build: ## Build the binary
	go build ./...
lint: ## Run linter
	golangci-lint run
`
	targets := parseMakefileTargets(content)
	want := map[string]bool{"build": true, "test": true, "lint": true}
	for _, tgt := range targets {
		delete(want, tgt)
	}
	for missing := range want {
		t.Errorf("missing target %q", missing)
	}
}

func TestParseTaskfileTargets(t *testing.T) {
	content := `version: '3'
tasks:
  build:
    cmds:
      - go build ./...
  test:
    cmds:
      - go test ./...
  lint:
    cmds:
      - golangci-lint run
`
	targets := parseTaskfileTargets(content)
	if len(targets) != 3 {
		t.Errorf("expected 3 targets, got %d: %v", len(targets), targets)
	}
}

func TestParseJustfileRecipes(t *testing.T) {
	content := `# justfile

build:
    go build ./...

test arg="./...":
    go test {{arg}}

@clean:
    rm -rf bin/
`
	recipes := parseJustfileRecipes(content)
	want := map[string]bool{"build": true, "test": true, "clean": true}
	for _, r := range recipes {
		delete(want, r)
	}
	for missing := range want {
		t.Errorf("missing recipe %q", missing)
	}
}

func TestDetectCI_GithubActions(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "ci.yml"), []byte("on: push\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ci := detectCI(dir)
	if len(ci) != 1 || ci[0].System != "github-actions" {
		t.Errorf("expected github-actions, got %v", ci)
	}
}

func TestDetectGoGenerate_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), []byte("package foo\n//go:generate stringer -type=Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !detectGoGenerate(dir) {
		t.Error("expected go:generate to be detected")
	}
}

func TestDetectGoGenerate_NotFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if detectGoGenerate(dir) {
		t.Error("expected no go:generate")
	}
}

func TestDetectBuildInfo_Mage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "magefile.go"), []byte("//go:build mage\npackage main\nfunc Build() error { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := DetectBuildInfo(dir, nil)
	if info.System != "mage" {
		t.Errorf("expected mage, got %q", info.System)
	}
}

func TestIsValidMakeTarget_RejectsArtifacts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"backslash", `\`, false},
		{"regex_fragment", `/^[a-zA-Z0-9_-]+`, false},
		{"empty", "", false},
		{"valid_simple", "test", true},
		{"valid_hyphen", "lint-fast", true},
		{"valid_underscore", "eval_setup", true},
		{"dollar_var", "$(FOO)", false},
		{"percent_pattern", "%.o", false},
		{"dot_target", ".DEFAULT", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidMakeTarget(tt.input); got != tt.want {
				t.Errorf("isValidMakeTarget(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectBuildInfo_DefaultGo(t *testing.T) {
	dir := t.TempDir()
	info := DetectBuildInfo(dir, nil)
	if info.System != "go" {
		t.Errorf("expected go, got %q", info.System)
	}
	if info.Build != "go build ./..." {
		t.Errorf("unexpected build command: %q", info.Build)
	}
}
