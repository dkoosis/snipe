// Package config handles global and project-level configuration for snipe.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents snipe configuration settings.
type Config struct {
	// Limit is the default result limit
	Limit int `json:"limit,omitempty"`
	// ContextLines is the default context lines around matches
	ContextLines int `json:"context_lines,omitempty"`
}

// defaultConfig returns the default configuration.
func defaultConfig() *Config {
	return &Config{
		Limit:        50,
		ContextLines: 3,
	}
}

// globalConfigPath returns the path to the global config file.
func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "snipe", "config.json"), nil
}

// projectConfigPath returns the path to the project config file.
func projectConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".snipe.json")
}

// Load loads and merges configuration from global and project sources.
// Project config overrides global config, which overrides defaults.
// Returns an error if a config file exists but cannot be parsed.
func Load(projectRoot string) (*Config, error) {
	cfg := defaultConfig()

	globalPath, err := globalConfigPath()
	if err == nil {
		globalCfg, err := loadFile(globalPath)
		if err != nil {
			return nil, fmt.Errorf("global config: %w", err)
		}
		cfg = merge(cfg, globalCfg)
	}

	if projectRoot != "" {
		projectPath := projectConfigPath(projectRoot)
		projectCfg, err := loadFile(projectPath)
		if err != nil {
			return nil, fmt.Errorf("project config: %w", err)
		}
		cfg = merge(cfg, projectCfg)
	}

	return cfg, nil
}

// loadFile loads a config from a JSON file.
// Returns (nil, nil) if the file does not exist.
// Returns a non-nil error for permission failures or malformed JSON.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from globalConfigPath/projectConfigPath
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &cfg, nil
}

// merge merges src into dst, with src values taking precedence for non-zero values.
func merge(dst, src *Config) *Config {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src
	}

	result := *dst

	if src.Limit > 0 {
		result.Limit = src.Limit
	}
	if src.ContextLines > 0 {
		result.ContextLines = src.ContextLines
	}

	return &result
}
