package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tinywideclouds/go-github-store/internal/api"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-llm/pkg/yaml/filter"
)

// --- Mocks ---

type mockFetcher struct {
	files    []github.SyncFile
	analysis *github.RepositoryAnalysis
	err      error
}

func (m *mockFetcher) FetchRepository(ctx context.Context, repo, branch string, rules *filter.FilterRules, sendEvent func(stage string, details map[string]any)) ([]github.SyncFile, error) {
	// Simulate an event emission during tests to ensure no panics occur
	if sendEvent != nil {
		sendEvent("mock_fetch", map[string]any{"status": "ok"})
	}
	return m.files, m.err
}

func (m *mockFetcher) AnalyzeRepository(ctx context.Context, repo, branch string) (*github.RepositoryAnalysis, error) {
	if m.analysis != nil {
		return m.analysis, m.err
	}
	// Default mock return
	return &github.RepositoryAnalysis{
		Repo:           repo,
		Branch:         branch,
		CommitSHA:      "mock-sha-123",
		TotalFiles:     10,
		TotalSizeBytes: 1024,
	}, m.err
}

type mockStore struct {
	savedCacheID   string
	savedCommitSHA string
	savedFiles     []github.SyncFile
	err            error

	cacheCreated      *store.CacheMetadata
	getCacheReturn    *store.CacheMetadata
	caches            []store.CacheMetadata
	fileMetas         []store.FileMetadata
	fileContentReturn string
	profiles          []store.Profile
	profileSaved      *store.Profile
	deletedID         string
}

func (m *mockStore) SaveSync(ctx context.Context, cacheID, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error {
	m.savedCacheID = cacheID
	m.savedCommitSHA = commitSHA
	m.savedFiles = files
	return m.err
}
func (m *mockStore) CreateCache(ctx context.Context, meta *store.CacheMetadata) error {
	m.cacheCreated = meta
	return m.err
}
func (m *mockStore) GetCache(ctx context.Context, cacheID string) (*store.CacheMetadata, error) {
	if m.getCacheReturn != nil {
		return m.getCacheReturn, m.err
	}
	return nil, m.err
}
func (m *mockStore) ListCaches(ctx context.Context) ([]store.CacheMetadata, error) {
	return m.caches, m.err
}
func (m *mockStore) GetFileContent(ctx context.Context, cacheID string, docID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.fileContentReturn, nil
}
func (m *mockStore) ListFilesMetadata(ctx context.Context, cacheID string) ([]store.FileMetadata, error) {
	return m.fileMetas, m.err
}
func (m *mockStore) ListProfiles(ctx context.Context, cacheID string) ([]store.Profile, error) {
	return m.profiles, m.err
}
func (m *mockStore) CreateProfile(ctx context.Context, cacheID string, profile *store.Profile) error {
	m.profileSaved = profile
	return m.err
}
func (m *mockStore) UpdateProfile(ctx context.Context, cacheID string, profile *store.Profile) error {
	m.profileSaved = profile
	return m.err
}
func (m *mockStore) DeleteProfile(ctx context.Context, cacheID, profileID string) error {
	m.deletedID = profileID
	return m.err
}

// Close satisfies the Store interface for testing.
func (m *mockStore) Close() error {
	return nil
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Tests ---

func TestCreateCacheHandler(t *testing.T) {
	logger := newTestLogger()
	fetcher := &mockFetcher{
		analysis: &github.RepositoryAnalysis{
			Repo:           "my-org/my-repo",
			Branch:         "main",
			CommitSHA:      "abc-123",
			TotalFiles:     5,
			TotalSizeBytes: 500,
		},
	}
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: fetcher, Store: storeMock, Logger: logger}

	reqBody := `{"repo":"my-org/my-repo", "branch":"main"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	apiHandler.CreateCacheHandler(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var res store.CacheMetadata
	json.NewDecoder(w.Body).Decode(&res)

	// Assert Response
	assert.Equal(t, "unsynced", res.Status)
	assert.Equal(t, "my-org/my-repo", res.Repo)
	assert.Equal(t, 5, res.Analysis.TotalFiles)

	// Assert Database was called
	assert.NotNil(t, storeMock.cacheCreated)
	assert.Equal(t, "abc-123", storeMock.cacheCreated.SyncedCommitSHA)
}

func TestSyncHandler(t *testing.T) {
	logger := newTestLogger()
	fetcher := &mockFetcher{files: []github.SyncFile{{Path: "main.go", Content: "code"}}}
	storeMock := &mockStore{
		getCacheReturn: &store.CacheMetadata{
			ID:              "cache-123",
			Repo:            "my-org/my-repo",
			Branch:          "main",
			SyncedCommitSHA: "abc-123",
			Status:          "unsynced",
		},
	}
	apiHandler := &api.API{Fetcher: fetcher, Store: storeMock, Logger: logger}

	reqBody := `{"ingestionRules": {"include": ["**/*.go"]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches/cache-123/sync", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", "cache-123")
	w := httptest.NewRecorder()

	apiHandler.SyncHandler(w, req)

	// The HTTP Flusher implicitly writes a 200 OK when headers are flushed.
	assert.Equal(t, http.StatusOK, w.Code)

	// Because this is now an SSE stream, we read the raw text stream
	bodyStr := w.Body.String()

	// Assert the SSE formatted stream includes our events
	assert.Contains(t, bodyStr, `"stage":"init"`)
	assert.Contains(t, bodyStr, `"stage":"complete"`)
	assert.Contains(t, bodyStr, `"filesProcessed":1`)

	// Assert database save sync was called correctly
	assert.Equal(t, "cache-123", storeMock.savedCacheID)
	assert.Equal(t, "abc-123", storeMock.savedCommitSHA)
}

func TestCreateProfileHandler_ValidYaml(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	reqBody := `{"name":"Backend", "rulesYaml":"include:\n  - \"**/*.go\""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches/cache-1/profiles", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", "cache-1")
	w := httptest.NewRecorder()

	apiHandler.CreateProfileHandler(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.NotNil(t, storeMock.profileSaved)
	assert.Equal(t, "Backend", storeMock.profileSaved.Name)
}

func TestCreateProfileHandler_InvalidYaml(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	// Deliberately broken YAML with a tab character where one shouldn't be
	reqBody := `{"name":"Backend", "rulesYaml":"include:\n\t- \"**/*.go\""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches/cache-1/profiles", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", "cache-1")
	w := httptest.NewRecorder()

	apiHandler.CreateProfileHandler(w, req)

	// Must reject with 400 Bad Request
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Nil(t, storeMock.profileSaved)

	var errResp map[string]string
	json.NewDecoder(w.Body).Decode(&errResp)
	assert.Contains(t, errResp["error"], "invalid YAML structure")
}

func TestListFilesMetadataHandler(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{
		fileMetas: []store.FileMetadata{{Path: "main.go", SizeBytes: 100, Extension: ".go"}},
	}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	req := httptest.NewRequest(http.MethodGet, "/v1/caches/cache-1/files", nil)
	req.SetPathValue("id", "cache-1")
	w := httptest.NewRecorder()

	apiHandler.ListFilesMetadataHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var res map[string][]store.FileMetadata
	json.NewDecoder(w.Body).Decode(&res)
	assert.Len(t, res["files"], 1)
	assert.Equal(t, "main.go", res["files"][0].Path)
}

func TestGetFileContentHandler_Success(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{
		fileContentReturn: "package main\n\nfunc main() {}",
	}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	req := httptest.NewRequest(http.MethodGet, "/v1/caches/cache-1/files/base64-encoded-path/content", nil)
	req.SetPathValue("id", "cache-1")
	req.SetPathValue("base64Path", "base64-encoded-path")
	w := httptest.NewRecorder()

	apiHandler.GetFileContentHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var res map[string]string
	json.NewDecoder(w.Body).Decode(&res)
	assert.Equal(t, "package main\n\nfunc main() {}", res["content"])
}

// NEW: Test GetFileContentHandler Not Found / DB Error
func TestGetFileContentHandler_NotFound(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{
		err: errors.New("firestore: document not found"),
	}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	req := httptest.NewRequest(http.MethodGet, "/v1/caches/cache-1/files/bad-path/content", nil)
	req.SetPathValue("id", "cache-1")
	req.SetPathValue("base64Path", "bad-path")
	w := httptest.NewRecorder()

	apiHandler.GetFileContentHandler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var res map[string]string
	json.NewDecoder(w.Body).Decode(&res)
	assert.Contains(t, res["error"], "File not found")
}
