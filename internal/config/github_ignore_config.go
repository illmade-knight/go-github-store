package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// GitHubIgnoreConfig holds the rules for filtering out irrelevant files during ingestion.
type GitHubIgnoreConfig struct {
	IgnoredDirs       []string `yaml:"ignored_dirs"`
	IgnoredExtensions []string `yaml:"ignored_extensions"`
	IgnoredFiles      []string `yaml:"ignored_files"`

	// Fast lookup maps generated after unmarshaling
	ExtMap  map[string]bool `yaml:"-"`
	FileMap map[string]bool `yaml:"-"`
}

// LoadGitHubIgnoreConfig parses the raw YAML bytes and builds the O(1) lookup maps.
func LoadGitHubIgnoreConfig(data []byte) (*GitHubIgnoreConfig, error) {
	var cfg GitHubIgnoreConfig
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
