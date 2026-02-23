package github_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-github-store/internal/config"
	"github.com/tinywideclouds/go-github-store/internal/github"
)

func mockIgnoreConfig() *config.GitHubIgnoreConfig {
	return &config.GitHubIgnoreConfig{
		IgnoredDirs: []string{"node_modules/", "vendor/", ".git/", "build/"},
		ExtMap: map[string]bool{
			".png": true, ".pdf": true, ".exe": true,
		},
		FileMap: map[string]bool{
			"package-lock.json": true, "go.sum": true,
		},
	}
}

func TestShouldIgnore(t *testing.T) {
	cfg := mockIgnoreConfig()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Valid files
		{"Go source", "cmd/main.go", false},
		{"Readme", "README.md", false},
		{"Nested valid", "pkg/domain/models/user.ts", false},

		// Ignored directories
		{"Root Node modules", "node_modules/express/index.js", true},
		{"Vendor dir", "vendor/golang.org/x/sync/errgroup.go", true},
		{"Build dir", "build/static/js/main.js", true},

		// Ignored extensions
		{"PNG image", "assets/logo.png", true},
		{"Executable", "bin/server.exe", true},

		// Ignored specific files
		{"NPM Lockfile", "package-lock.json", true},
		{"Go sum", "go.sum", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := github.ShouldIgnore(tt.path, cfg)
			assert.Equal(t, tt.expected, result, "Path: %s", tt.path)
		})
	}
}
