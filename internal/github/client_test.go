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

func TestFetchRepository(t *testing.T) {
	// 1. Setup Mock GitHub API Server
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/test-org/test-repo/git/trees/main", func(w http.ResponseWriter, r *http.Request) {
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
				{Path: "node_modules/index.js", Type: "blob", Sha: "sha-ignore-1", Size: 200}, // Should ignore
				{Path: "logo.png", Type: "blob", Sha: "sha-ignore-2", Size: 300},              // Should ignore
				{Path: "utils.go", Type: "blob", Sha: "sha-utils-go", Size: 150},
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	mux.HandleFunc("/repos/test-org/test-repo/git/blobs/sha-main-go", func(w http.ResponseWriter, r *http.Request) {
		content := base64.StdEncoding.EncodeToString([]byte("package main"))
		json.NewEncoder(w).Encode(gitBlobResponse{Content: content, Encoding: "base64", Size: 100})
	})

	mux.HandleFunc("/repos/test-org/test-repo/git/blobs/sha-utils-go", func(w http.ResponseWriter, r *http.Request) {
		// Simulate GitHub's multi-line base64 formatting
		content := base64.StdEncoding.EncodeToString([]byte("package utils\n\nfunc Run() {}"))
		json.NewEncoder(w).Encode(gitBlobResponse{Content: content + "\n", Encoding: "base64", Size: 150})
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 2. Setup Client with Mock Server URL
	client := NewClient("mock-token", mockIgnoreConfig(), newTestLogger())
	client.baseURL = ts.URL // Override for testing

	// 3. Execute Fetch
	files, err := client.FetchRepository(context.Background(), "test-org/test-repo", "main")

	// 4. Assertions
	require.NoError(t, err)
	assert.Len(t, files, 2, "Should have fetched exactly 2 valid files, ignoring the others")

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
}
