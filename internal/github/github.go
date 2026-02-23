package github

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/tinywideclouds/go-github-store/internal/config"
)

// SyncFile represents a single processed file ready to be written to the data store.
type SyncFile struct {
	Path      string
	Content   string
	SizeBytes int
	Status    string // Typically "included"
	Extension string
}

// Fetcher defines the contract for retrieving and processing a repository tree.
type Fetcher interface {
	FetchRepository(ctx context.Context, repo, branch string) ([]SyncFile, error)
}

// ShouldIgnore evaluates the file path against the provided configuration
// and returns true if it should be excluded from the sync process.
func ShouldIgnore(path string, cfg *config.GitHubIgnoreConfig) bool {
	if cfg == nil {
		return false // Default to safe inclusion if configuration is missing
	}

	normalizedPath := filepath.ToSlash(path)

	// 1. Check for blacklisted directories anywhere in the path
	for _, dir := range cfg.IgnoredDirs {
		if strings.Contains(normalizedPath, "/"+dir) || strings.HasPrefix(normalizedPath, dir) {
			return true
		}
	}

	// 2. Check exact file names
	baseName := filepath.Base(normalizedPath)
	if cfg.FileMap[baseName] {
		return true
	}

	// 3. Check extensions (ignoring case)
	ext := strings.ToLower(filepath.Ext(normalizedPath))
	if cfg.ExtMap[ext] {
		return true
	}

	return false
}
