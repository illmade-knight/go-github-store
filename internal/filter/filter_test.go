package filter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-github-store/internal/filter"
)

func TestParseYAML(t *testing.T) {
	t.Run("Valid YAML", func(t *testing.T) {
		yamlStr := `
include:
  - "**/*.go"
  - "*.md"
exclude:
  - "vendor/**"
`
		rules, err := filter.ParseYAML(yamlStr)
		require.NoError(t, err)
		assert.Len(t, rules.Include, 2)
		assert.Equal(t, "**/*.go", rules.Include[0])
		assert.Len(t, rules.Exclude, 1)
		assert.Equal(t, "vendor/**", rules.Exclude[0])
	})

	t.Run("Invalid YAML", func(t *testing.T) {
		// Tabs are illegal for indentation in YAML
		yamlStr := "include:\n\t- \"**/*.go\""
		_, err := filter.ParseYAML(yamlStr)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid YAML structure")
	})
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		rules    filter.FilterRules
		path     string
		expected bool
	}{
		{
			name:     "Empty rules allow everything by default",
			rules:    filter.FilterRules{},
			path:     "main.go",
			expected: true,
		},
		{
			name: "Simple include match",
			rules: filter.FilterRules{
				Include: []string{"*.go"},
			},
			path:     "main.go",
			expected: true,
		},
		{
			name: "Simple include miss",
			rules: filter.FilterRules{
				Include: []string{"*.go"},
			},
			path:     "readme.md",
			expected: false, // Because includes are defined, defaults to false
		},
		{
			name: "Doublestar recursive include match",
			rules: filter.FilterRules{
				Include: []string{"**/*.go"},
			},
			path:     "internal/api/handlers/sync.go",
			expected: true,
		},
		{
			name: "Exclude exact match",
			rules: filter.FilterRules{
				Exclude: []string{"secret.yaml"},
			},
			path:     "secret.yaml",
			expected: false,
		},
		{
			name: "Exclude overrides include",
			rules: filter.FilterRules{
				Include: []string{"**/*.go"},
				Exclude: []string{"vendor/**"},
			},
			path:     "vendor/github.com/some/package/main.go",
			expected: false,
		},
		{
			name: "Multiple includes match second option",
			rules: filter.FilterRules{
				Include: []string{"**/*.ts", "**/*.md"},
			},
			path:     "docs/readme.md",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rules.Match(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
