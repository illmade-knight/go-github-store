package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-github-store/internal/config"
)

func TestLoadGitHubIgnoreConfig(t *testing.T) {
	t.Run("Success - parses YAML and builds fast lookup maps", func(t *testing.T) {
		yamlData := []byte(`
ignored_dirs:
  - "node_modules/"
  - ".git/"
ignored_extensions:
  - ".png"
  - ".pdf"
ignored_files:
  - "package-lock.json"
  - "go.sum"
`)

		cfg, err := config.LoadGitHubIgnoreConfig(yamlData)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		// 1. Verify the raw slices were populated
		assert.Equal(t, []string{"node_modules/", ".git/"}, cfg.IgnoredDirs)
		assert.Equal(t, []string{".png", ".pdf"}, cfg.IgnoredExtensions)
		assert.Equal(t, []string{"package-lock.json", "go.sum"}, cfg.IgnoredFiles)

		// 2. Verify the Extension O(1) lookup map was built correctly
		assert.True(t, cfg.ExtMap[".png"])
		assert.True(t, cfg.ExtMap[".pdf"])
		assert.False(t, cfg.ExtMap[".go"]) // Ensure false positives don't happen

		// 3. Verify the File O(1) lookup map was built correctly
		assert.True(t, cfg.FileMap["package-lock.json"])
		assert.True(t, cfg.FileMap["go.sum"])
		assert.False(t, cfg.FileMap["main.go"])
	})

	t.Run("Failure - handles malformed YAML", func(t *testing.T) {
		badYamlData := []byte(`
ignored_dirs:
  - "node_modules/"
    bad_indentation: [
`)

		cfg, err := config.LoadGitHubIgnoreConfig(badYamlData)

		require.Error(t, err)
		require.Nil(t, cfg)
		assert.Contains(t, err.Error(), "failed to parse github ignore yaml")
	})
}
