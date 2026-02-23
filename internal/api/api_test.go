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
}

func (m *mockStore) SaveSync(ctx context.Context, cacheID, repo, branch string, files []github.SyncFile) error {
	m.savedCacheID = cacheID
	m.savedFiles = files
	return m.err
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Tests ---

func TestSyncHandler(t *testing.T) {
	logger := newTestLogger()

	t.Run("Success - existing cache ID", func(t *testing.T) {
		fetcher := &mockFetcher{
			files: []github.SyncFile{{Path: "main.go", Content: "code"}},
		}
		store := &mockStore{}
		apiHandler := &api.API{Fetcher: fetcher, Store: store, Logger: logger}

		reqBody := `{"repo":"my-org/my-repo", "branch":"main", "cacheId":"custom-123"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/caches/sync", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		apiHandler.SyncHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var res api.SyncResponse
		json.NewDecoder(w.Body).Decode(&res)

		assert.Equal(t, "custom-123", res.CacheID)
		assert.Equal(t, "success", res.Status)
		assert.Equal(t, 1, res.FilesProcessed)

		// Verify the store received the correct data
		assert.Equal(t, "custom-123", store.savedCacheID)
		assert.Len(t, store.savedFiles, 1)
	})

	t.Run("Success - generates new cache ID if omitted", func(t *testing.T) {
		fetcher := &mockFetcher{}
		store := &mockStore{}
		apiHandler := &api.API{Fetcher: fetcher, Store: store, Logger: logger}

		reqBody := `{"repo":"my-org/my-repo", "branch":"main"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/caches/sync", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		apiHandler.SyncHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var res api.SyncResponse
		json.NewDecoder(w.Body).Decode(&res)

		assert.NotEmpty(t, res.CacheID)
		assert.Contains(t, res.CacheID, "urn:llm:cache:")
	})

	t.Run("Failure - missing required fields", func(t *testing.T) {
		apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: &mockStore{}, Logger: logger}

		// Missing branch
		reqBody := `{"repo":"my-org/my-repo"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/caches/sync", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		apiHandler.SyncHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Failure - GitHub fetch errors out", func(t *testing.T) {
		fetcher := &mockFetcher{err: errors.New("github rate limit")}
		apiHandler := &api.API{Fetcher: fetcher, Store: &mockStore{}, Logger: logger}

		reqBody := `{"repo":"my-org/my-repo", "branch":"main"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/caches/sync", bytes.NewBufferString(reqBody))
		w := httptest.NewRecorder()

		apiHandler.SyncHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
