package config

import (
	"fmt"
	"gopkg.in/yaml.v3"

	"github.com/tinywideclouds/go-github-store/internal/github"
)

// LoadGitHubIgnoreConfig parses the raw YAML bytes and builds the O(1) lookup maps.
func LoadGitHubIgnoreConfig(data []byte) (*github.GitHubIgnoreConfig, error) {
	var cfg github.GitHubIgnoreConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse github ignore yaml: %w", err)
	}

	// Build fast lookup maps for extensions
	cfg.ExtMap = make(map[string]bool)
	for _, ext := range cfg.IgnoredExtensions {
		cfg.ExtMap[ext] = true
	}

	// Build fast lookup maps for specific filenames
	cfg.FileMap = make(map[string]bool)
	for _, f := range cfg.IgnoredFiles {
		cfg.FileMap[f] = true
	}

	return &cfg, nil
}
