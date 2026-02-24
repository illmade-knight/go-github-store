package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tinywideclouds/go-github-store/internal/api"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
)

// --- Mocks ---

type mockFetcher struct {
	files []github.SyncFile
	err   error
}

func (m *mockFetcher) FetchRepository(ctx context.Context, repo, branch string) ([]github.SyncFile, error) {
	return m.files, m.err
}

type mockStore struct {
	savedCacheID string
	savedFiles   []github.SyncFile
	err          error

	caches       []store.CacheMetadata
	fileMetas    []store.FileMetadata
	profiles     []store.Profile
	profileSaved *store.Profile
	deletedID    string
}

func (m *mockStore) SaveSync(ctx context.Context, cacheID, repo, branch string, files []github.SyncFile) error {
	m.savedCacheID = cacheID
	m.savedFiles = files
	return m.err
}
func (m *mockStore) ListCaches(ctx context.Context) ([]store.CacheMetadata, error) {
	return m.caches, m.err
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

func TestSyncHandler(t *testing.T) {
	logger := newTestLogger()
	fetcher := &mockFetcher{files: []github.SyncFile{{Path: "main.go", Content: "code"}}}
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: fetcher, Store: storeMock, Logger: logger}

	reqBody := `{"repo":"my-org/my-repo", "branch":"main", "cacheId":"custom-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches/sync", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	apiHandler.SyncHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var res api.SyncResponse
	json.NewDecoder(w.Body).Decode(&res)
	assert.Equal(t, "success", res.Status)
}

func TestCreateProfileHandler_ValidYaml(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	reqBody := `{"name":"Backend", "rulesYaml":"include:\n  - \"**/*.go\""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/caches/cache-1/profiles", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", "cache-1") // Simulating standard lib path values
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
	assert.Contains(t, errResp["error"], "Invalid YAML format")
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
