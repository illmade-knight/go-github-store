package github

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/tinywideclouds/go-github-store/internal/config"
	"github.com/tinywideclouds/go-github-store/internal/filter"
)

// SyncFile represents a single processed file ready to be written to the data store.
type SyncFile struct {
	Path      string
	Content   string
	SizeBytes int
	Status    string // Typically "included"
	Extension string
}

// RepositoryAnalysis contains metadata from a lightweight tree traversal.
type RepositoryAnalysis struct {
	Repo           string
	Branch         string // The explicitly resolved branch (e.g., if empty was passed, this holds "main")
	CommitSHA      string // The SHA of the tree analyzed
	TotalFiles     int
	TotalSizeBytes int
	Extensions     map[string]int
}

// Fetcher defines the contract for retrieving and processing a repository tree.
type Fetcher interface {
	FetchRepository(ctx context.Context, repo, branch string, rules *filter.FilterRules) ([]SyncFile, error)
	// AnalyzeRepository performs a lightweight fetch to calculate file metrics without downloading contents.
	AnalyzeRepository(ctx context.Context, repo, branch string) (*RepositoryAnalysis, error)
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
