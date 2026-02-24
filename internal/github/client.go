package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/tinywideclouds/go-github-store/internal/config"
	"github.com/tinywideclouds/go-github-store/internal/filter"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	maxConcurrency   = 10 // Bounded concurrency limit for blob fetching
)

// Client implements the Fetcher interface to interact with the GitHub REST API.
type Client struct {
	baseURL      string
	token        string
	httpClient   *http.Client
	ignoreConfig *config.GitHubIgnoreConfig
	logger       *slog.Logger
}

// NewClient creates a new GitHub API client.
func NewClient(token string, ignoreConfig *config.GitHubIgnoreConfig, logger *slog.Logger) *Client {
	return &Client{
		baseURL:      defaultGitHubAPI,
		token:        token,
		httpClient:   &http.Client{},
		ignoreConfig: ignoreConfig,
		logger:       logger,
	}
}

// gitTreeResponse represents the payload from the recursive tree API.
type gitTreeResponse struct {
	Sha       string `json:"sha"`
	Url       string `json:"url"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"` // "blob" or "tree"
		Sha  string `json:"sha"`
		Size int    `json:"size"`
	} `json:"tree"`
}

// gitBlobResponse represents the payload from the blob API.
type gitBlobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // usually "base64"
	Size     int    `json:"size"`
}

// resolveBranchAndSHA gets the default branch (if empty) and the latest commit SHA.
func (c *Client) resolveBranchAndSHA(ctx context.Context, repo, branch string) (string, string, error) {
	actualBranch := branch

	// 1. Resolve default branch if none was provided
	if actualBranch == "" {
		repoURL := fmt.Sprintf("%s/repos/%s", c.baseURL, repo)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL, nil)
		if err != nil {
			return "", "", err
		}
		c.setHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("status %d fetching repo info", resp.StatusCode)
		}

		var repoData struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&repoData); err != nil {
			return "", "", err
		}
		actualBranch = repoData.DefaultBranch
	}

	// 2. Resolve the latest Commit SHA for the specific branch
	commitURL := fmt.Sprintf("%s/repos/%s/commits/%s", c.baseURL, repo, actualBranch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, commitURL, nil)
	if err != nil {
		return "", "", err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("status %d fetching commit info", resp.StatusCode)
	}

	var commitData struct {
		Sha string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commitData); err != nil {
		return "", "", err
	}

	return actualBranch, commitData.Sha, nil
}

// AnalyzeRepository performs a lightweight fetch to calculate file metrics without downloading contents.
func (c *Client) AnalyzeRepository(ctx context.Context, repo, branch string) (*RepositoryAnalysis, error) {
	c.logger.Info("Analyzing repository", "repo", repo, "requested_branch", branch)

	actualBranch, commitSHA, err := c.resolveBranchAndSHA(ctx, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve branch and sha: %w", err)
	}

	treeURL := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.baseURL, repo, commitSHA)
	treeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tree request: %w", err)
	}
	c.setHeaders(treeReq)

	resp, err := c.httpClient.Do(treeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tree request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API error fetching tree: status %d", resp.StatusCode)
	}

	var treeData gitTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeData); err != nil {
		return nil, fmt.Errorf("failed to decode tree response: %w", err)
	}

	if treeData.Truncated {
		c.logger.Warn("Repository tree is truncated (too large for recursive fetch)", "repo", repo)
	}

	analysis := &RepositoryAnalysis{
		Repo:           repo,
		Branch:         actualBranch,
		CommitSHA:      commitSHA,
		TotalFiles:     0,
		TotalSizeBytes: 0,
		Extensions:     make(map[string]int),
	}

	for _, item := range treeData.Tree {
		if item.Type == "blob" && !ShouldIgnore(item.Path, c.ignoreConfig) {
			analysis.TotalFiles++
			analysis.TotalSizeBytes += item.Size

			ext := ""
			if idx := strings.LastIndex(item.Path, "."); idx != -1 {
				ext = strings.ToLower(item.Path[idx:])
				analysis.Extensions[ext]++
			}
		}
	}

	c.logger.Info("Repository analyzed", "repo", repo, "total_files", analysis.TotalFiles, "total_size_bytes", analysis.TotalSizeBytes)
	return analysis, nil
}

// FetchRepository orchestrates fetching the tree and downloading the allowed blobs.
func (c *Client) FetchRepository(ctx context.Context, repo, branch string, rules *filter.FilterRules) ([]SyncFile, error) {
	c.logger.Info("Fetching repository tree for sync", "repo", repo, "requested_branch", branch)

	// Ensure we are working with the exact branch and SHA even if defaults were requested
	actualBranch, commitSHA, err := c.resolveBranchAndSHA(ctx, repo, branch)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve branch and sha: %w", err)
	}

	// 1. Fetch the entire tree recursively using the precise Commit SHA
	treeURL := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.baseURL, repo, commitSHA)
	treeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tree request: %w", err)
	}
	c.setHeaders(treeReq)

	resp, err := c.httpClient.Do(treeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute tree request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API error fetching tree: status %d", resp.StatusCode)
	}

	var treeData gitTreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeData); err != nil {
		return nil, fmt.Errorf("failed to decode tree response: %w", err)
	}

	if treeData.Truncated {
		c.logger.Warn("Repository tree is truncated (too large for recursive fetch)", "repo", repo, "branch", actualBranch)
	}

	// 2. Filter the tree and prepare for concurrent blob fetching
	var validBlobs []struct {
		Path, Sha string
		Size      int
	}
	for _, item := range treeData.Tree {
		// First pass: global system ignores (e.g., node_modules, binary extensions)
		if item.Type == "blob" && !ShouldIgnore(item.Path, c.ignoreConfig) {
			// Second pass: apply Dynamic Ingestion Filter Rules
			if rules == nil || rules.Match(item.Path) {
				validBlobs = append(validBlobs, struct {
					Path, Sha string
					Size      int
				}{
					Path: item.Path, Sha: item.Sha, Size: item.Size,
				})
			}
		}
	}

	c.logger.Info("Tree fetched and filtered", "total_items", len(treeData.Tree), "valid_blobs", len(validBlobs))

	// 3. Concurrently fetch blobs
	results := make([]SyncFile, 0, len(validBlobs))
	var mu sync.Mutex // Protects the results slice
	var wg sync.WaitGroup

	// Semaphore to limit concurrent HTTP requests
	sem := make(chan struct{}, maxConcurrency)
	errChan := make(chan error, len(validBlobs))

	for _, blobInfo := range validBlobs {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot

		go func(path, sha string, size int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot

			content, err := c.fetchBlob(ctx, repo, sha)
			if err != nil {
				errChan <- fmt.Errorf("failed to fetch blob %s: %w", path, err)
				return
			}

			// Extract extension for easy querying later
			ext := ""
			if idx := strings.LastIndex(path, "."); idx != -1 {
				ext = strings.ToLower(path[idx:])
			}

			syncFile := SyncFile{
				Path:      path,
				Content:   content,
				SizeBytes: size,
				Status:    "included",
				Extension: ext,
			}

			mu.Lock()
			results = append(results, syncFile)
			mu.Unlock()
		}(blobInfo.Path, blobInfo.Sha, blobInfo.Size)
	}

	wg.Wait()
	close(errChan)

	// If any blob failed, fail the whole operation (we want an atomic sync)
	if err, ok := <-errChan; ok {
		return nil, err
	}

	return results, nil
}

func (c *Client) fetchBlob(ctx context.Context, repo, sha string) (string, error) {
	blobURL := fmt.Sprintf("%s/repos/%s/git/blobs/%s", c.baseURL, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var blobData gitBlobResponse
	if err := json.NewDecoder(resp.Body).Decode(&blobData); err != nil {
		return "", err
	}

	if blobData.Encoding == "base64" {
		// GitHub base64 responses contain newlines, which standard base64 decoder can choke on.
		cleanBase64 := strings.ReplaceAll(blobData.Content, "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(cleanBase64)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64: %w", err)
		}
		return string(decoded), nil
	}

	return blobData.Content, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
