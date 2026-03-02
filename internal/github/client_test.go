package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-github-store/internal/config"

	"github.com/tinywideclouds/go-llm/pkg/yaml/filter"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mockIgnoreConfig() *config.GitHubIgnoreConfig {
	return &config.GitHubIgnoreConfig{
		IgnoredDirs: []string{"node_modules/"},
		ExtMap:      map[string]bool{".png": true},
		FileMap:     map[string]bool{"package-lock.json": true},
	}
}

// setupMockGitHubServer creates a mock server that simulates branch resolution, commit lookup, and tree fetching.
func setupMockGitHubServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	// 1. Mock Default Branch Resolution
	mux.HandleFunc("/repos/test-org/test-repo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"default_branch": "main"})
	})

	// 2. Mock Commit SHA Resolution
	mux.HandleFunc("/repos/test-org/test-repo/commits/main", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"sha": "mock-commit-sha"})
	})

	// 3. Mock Git Trees API (Using the resolved Commit SHA)
	mux.HandleFunc("/repos/test-org/test-repo/git/trees/mock-commit-sha", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("recursive"))
		assert.Equal(t, "Bearer mock-token", r.Header.Get("Authorization"))

		response := gitTreeResponse{
			Sha: "tree-sha",
			Tree: []struct {
				Path string `json:"path"`
				Mode string `json:"mode"`
				Type string `json:"type"`
				Sha  string `json:"sha"`
				Size int    `json:"size"`
			}{
				{Path: "main.go", Type: "blob", Sha: "sha-main-go", Size: 100},
				{Path: "node_modules/index.js", Type: "blob", Sha: "sha-ignore-1", Size: 200}, // Should ignore (Global)
				{Path: "logo.png", Type: "blob", Sha: "sha-ignore-2", Size: 300},              // Should ignore (Global)
				{Path: "utils.go", Type: "blob", Sha: "sha-utils-go", Size: 150},
				{Path: "test_data.json", Type: "blob", Sha: "sha-json", Size: 50}, // Should ignore (Dynamic Rules)
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	// 4. Mock Blobs
	mux.HandleFunc("/repos/test-org/test-repo/git/blobs/sha-main-go", func(w http.ResponseWriter, r *http.Request) {
		content := base64.StdEncoding.EncodeToString([]byte("package main"))
		json.NewEncoder(w).Encode(gitBlobResponse{Content: content, Encoding: "base64", Size: 100})
	})

	mux.HandleFunc("/repos/test-org/test-repo/git/blobs/sha-utils-go", func(w http.ResponseWriter, r *http.Request) {
		content := base64.StdEncoding.EncodeToString([]byte("package utils\n\nfunc Run() {}"))
		json.NewEncoder(w).Encode(gitBlobResponse{Content: content + "\n", Encoding: "base64", Size: 150})
	})

	return httptest.NewServer(mux)
}

func TestAnalyzeRepository(t *testing.T) {
	ts := setupMockGitHubServer(t)
	defer ts.Close()

	client := NewClient("mock-token", mockIgnoreConfig(), newTestLogger())
	client.baseURL = ts.URL

	// Execute Analyze (Passing empty string to test default branch resolution)
	analysis, err := client.AnalyzeRepository(context.Background(), "test-org/test-repo", "")

	require.NoError(t, err)
	assert.Equal(t, "main", analysis.Branch, "Should have resolved the default branch")
	assert.Equal(t, "mock-commit-sha", analysis.CommitSHA)
	assert.Equal(t, 3, analysis.TotalFiles, "Should have counted main.go, utils.go, test_data.json")
	assert.Equal(t, 300, analysis.TotalSizeBytes) // 100 + 150 + 50
	assert.Equal(t, 2, analysis.Extensions[".go"])
	assert.Equal(t, 1, analysis.Extensions[".json"])
}

func TestFetchRepository(t *testing.T) {
	ts := setupMockGitHubServer(t)
	defer ts.Close()

	client := NewClient("mock-token", mockIgnoreConfig(), newTestLogger())
	client.baseURL = ts.URL

	// Execute Fetch with Dynamic Ingestion Rules (Exclude .json files)
	rules := &filter.FilterRules{
		Exclude: []string{"**/*.json"},
	}

	// Pass `nil` for the sendEvent callback in tests where we aren't asserting on the events
	files, err := client.FetchRepository(context.Background(), "test-org/test-repo", "main", rules, nil)

	// Assertions
	require.NoError(t, err)
	assert.Len(t, files, 2, "Should have fetched exactly 2 valid files, ignoring global and dynamic ignores")

	// Order is non-deterministic due to concurrency, so we map them
	fileMap := make(map[string]SyncFile)
	for _, f := range files {
		fileMap[f.Path] = f
	}

	assert.Contains(t, fileMap, "main.go")
	assert.Equal(t, "package main", fileMap["main.go"].Content)
	assert.Equal(t, ".go", fileMap["main.go"].Extension)

	assert.Contains(t, fileMap, "utils.go")
	assert.Equal(t, "package utils\n\nfunc Run() {}", fileMap["utils.go"].Content)
	assert.Equal(t, ".go", fileMap["utils.go"].Extension)

	assert.NotContains(t, fileMap, "test_data.json", "Dynamic rules should have excluded this file")
}
