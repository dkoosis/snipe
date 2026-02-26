package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Limit != 50 {
		t.Errorf("Limit = %d, want 50", cfg.Limit)
	}
	if cfg.ContextLines != 3 {
		t.Errorf("ContextLines = %d, want 3", cfg.ContextLines)
	}
}

func TestLoad_NoConfigFiles(t *testing.T) {
	// With no config files, should return defaults
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Limit != 50 {
		t.Errorf("Limit = %d, want 50", cfg.Limit)
	}
}

func TestLoad_ProjectConfigOnly(t *testing.T) {
	dir := t.TempDir()
	projectCfg := &Config{
		Limit:        100,
		ContextLines: 5,
	}
	writeConfig(t, filepath.Join(dir, ".snipe.json"), projectCfg)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Limit != 100 {
		t.Errorf("Limit = %d, want 100", cfg.Limit)
	}
	if cfg.ContextLines != 5 {
		t.Errorf("ContextLines = %d, want 5", cfg.ContextLines)
	}
}

func TestLoad_ProjectOverridesGlobal(t *testing.T) {
	// Create temp home dir for global config
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create global config
	globalDir := filepath.Join(homeDir, ".config", "snipe")
	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	globalCfg := &Config{
		Limit:        200,
		ContextLines: 10,
	}
	writeConfig(t, filepath.Join(globalDir, "config.json"), globalCfg)

	// Create project config that overrides some values
	projectDir := t.TempDir()
	projectCfg := &Config{
		Limit: 75, // Override global
		// ContextLines: 0 - don't override, use global
	}
	writeConfig(t, filepath.Join(projectDir, ".snipe.json"), projectCfg)

	cfg, err := Load(projectDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Project overrides global
	if cfg.Limit != 75 {
		t.Errorf("Limit = %d, want 75 (project override)", cfg.Limit)
	}

	// Global value used when project doesn't specify
	if cfg.ContextLines != 10 {
		t.Errorf("ContextLines = %d, want 10 (from global)", cfg.ContextLines)
	}
}

func TestProjectConfigPath(t *testing.T) {
	got := ProjectConfigPath("/home/user/project")
	want := filepath.Join("/home/user/project", ".snipe.json")
	if got != want {
		t.Errorf("ProjectConfigPath() = %q, want %q", got, want)
	}
}

func writeConfig(t *testing.T, path string, cfg *Config) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
