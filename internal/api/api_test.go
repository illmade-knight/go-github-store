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

	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	"github.com/tinywideclouds/go-data-sources/pkg/yaml"

	"github.com/tinywideclouds/go-github-store/internal/api"
	"github.com/tinywideclouds/go-github-store/internal/github"
	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

// --- Helpers ---

func mustURN(s string) urn.URN {
	u, err := urn.Parse(s)
	if err != nil {
		panic("invalid test URN: " + s)
	}
	return u
}

// --- Mocks ---

type mockFetcher struct {
	files    []github.SyncFile
	analysis *github.RepositoryAnalysis
	err      error
}

func (m *mockFetcher) FetchRepository(ctx context.Context, repo, branch string, rules *yaml.FilterRules, sendEvent func(stage string, details map[string]any)) ([]github.SyncFile, error) {
	if sendEvent != nil {
		sendEvent("mock_fetch", map[string]any{"status": "ok"})
	}
	return m.files, m.err
}

func (m *mockFetcher) AnalyzeRepository(ctx context.Context, repo, branch string) (*github.RepositoryAnalysis, error) {
	if m.analysis != nil {
		return m.analysis, m.err
	}
	return &github.RepositoryAnalysis{
		Repo:           repo,
		Branch:         branch,
		CommitSHA:      "mock-sha-123",
		TotalFiles:     10,
		TotalSizeBytes: 1024,
	}, m.err
}

type mockStore struct {
	savedDsID      urn.URN
	savedCommitSHA string
	savedFiles     []github.SyncFile
	err            error

	dsCreated         *datasources.DataSourceMetadata
	getDsReturn       *datasources.DataSourceMetadata
	dataSources       []datasources.DataSourceMetadata
	fileMetas         []datasources.FileMetadata
	fileContentReturn string
	profiles          []datasources.Profile
	profileSaved      *datasources.Profile
	deletedID         urn.URN
}

func (m *mockStore) SaveSync(ctx context.Context, dsID urn.URN, repo, branch, commitSHA string, files []github.SyncFile, sendEvent func(stage string, details map[string]any)) error {
	m.savedDsID = dsID
	m.savedCommitSHA = commitSHA
	m.savedFiles = files
	return m.err
}
func (m *mockStore) CreateDataSource(ctx context.Context, meta *datasources.DataSourceMetadata) error {
	m.dsCreated = meta
	return m.err
}
func (m *mockStore) GetDataSource(ctx context.Context, dsID urn.URN) (*datasources.DataSourceMetadata, error) {
	if m.getDsReturn != nil {
		return m.getDsReturn, m.err
	}
	return nil, m.err
}
func (m *mockStore) ListDataSources(ctx context.Context) ([]datasources.DataSourceMetadata, error) {
	return m.dataSources, m.err
}
func (m *mockStore) GetFileContent(ctx context.Context, dsID urn.URN, docID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.fileContentReturn, nil
}
func (m *mockStore) ListFilesMetadata(ctx context.Context, dsID urn.URN) ([]datasources.FileMetadata, error) {
	return m.fileMetas, m.err
}
func (m *mockStore) ListProfiles(ctx context.Context, dsID urn.URN) ([]datasources.Profile, error) {
	return m.profiles, m.err
}
func (m *mockStore) CreateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	m.profileSaved = profile
	return m.err
}
func (m *mockStore) UpdateProfile(ctx context.Context, dsID urn.URN, profile *datasources.Profile) error {
	m.profileSaved = profile
	return m.err
}
func (m *mockStore) DeleteProfile(ctx context.Context, dsID, profileID urn.URN) error {
	m.deletedID = profileID
	return m.err
}
func (m *mockStore) Close() error { return nil }

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- Tests ---

func TestCreateDataSourceHandler(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/v1/data-sources", bytes.NewBufferString(reqBody))
	w := httptest.NewRecorder()

	apiHandler.CreateDataSourceHandler(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var res datasources.DataSourceMetadata
	json.NewDecoder(w.Body).Decode(&res)

	assert.Equal(t, "unsynced", res.Status)
	assert.Equal(t, "my-org/my-repo", res.Repo)
	assert.Equal(t, int32(5), res.Analysis.TotalFiles)

	assert.NotNil(t, storeMock.dsCreated)
	assert.Equal(t, "abc-123", storeMock.dsCreated.SyncedCommitSha)
	assert.Contains(t, storeMock.dsCreated.ID, "urn:data-source:")
}

func TestSyncHandler(t *testing.T) {
	logger := newTestLogger()
	fetcher := &mockFetcher{files: []github.SyncFile{{Path: "main.go", Content: "code"}}}

	validDsURN := mustURN("urn:data-source:123")
	storeMock := &mockStore{
		getDsReturn: &datasources.DataSourceMetadata{
			ID:              validDsURN.String(),
			Repo:            "my-org/my-repo",
			Branch:          "main",
			SyncedCommitSha: "abc-123",
			Status:          "unsynced",
		},
	}
	apiHandler := &api.API{Fetcher: fetcher, Store: storeMock, Logger: logger}

	reqBody := `{"ingestionRules": {"include": ["**/*.go"]}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/data-sources/urn:data-source:123/sync", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", validDsURN.String())
	w := httptest.NewRecorder()

	apiHandler.SyncHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	bodyStr := w.Body.String()

	assert.Contains(t, bodyStr, `"stage":"init"`)
	assert.Contains(t, bodyStr, `"stage":"complete"`)
	assert.Contains(t, bodyStr, `"filesProcessed":1`)

	assert.Equal(t, validDsURN, storeMock.savedDsID)
	assert.Equal(t, "abc-123", storeMock.savedCommitSHA)
}

func TestCreateProfileHandler_ValidYaml(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	validDsURN := mustURN("urn:data-source:1")
	reqBody := `{"name":"Backend", "rulesYaml":"include:\n  - \"**/*.go\""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/data-sources/urn:data-source:1/profiles", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", validDsURN.String())
	w := httptest.NewRecorder()

	apiHandler.CreateProfileHandler(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.NotNil(t, storeMock.profileSaved)
	assert.Equal(t, "Backend", storeMock.profileSaved.Name)
	assert.Contains(t, storeMock.profileSaved.ID, "urn:profile:")
}

func TestCreateProfileHandler_InvalidYaml(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	validDsURN := mustURN("urn:data-source:1")
	reqBody := `{"name":"Backend", "rulesYaml":"include:\n\t- \"**/*.go\""}`
	req := httptest.NewRequest(http.MethodPost, "/v1/data-sources/urn:data-source:1/profiles", bytes.NewBufferString(reqBody))
	req.SetPathValue("id", validDsURN.String())
	w := httptest.NewRecorder()

	apiHandler.CreateProfileHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Nil(t, storeMock.profileSaved)

	var errResp map[string]string
	json.NewDecoder(w.Body).Decode(&errResp)
	assert.Contains(t, errResp["error"], "invalid YAML structure")
}

func TestListFilesMetadataHandler(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{
		fileMetas: []datasources.FileMetadata{{Path: "main.go", SizeBytes: 100, Extension: ".go"}},
	}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	validDsURN := mustURN("urn:data-source:1")
	req := httptest.NewRequest(http.MethodGet, "/v1/data-sources/urn:data-source:1/files", nil)
	req.SetPathValue("id", validDsURN.String())
	w := httptest.NewRecorder()

	apiHandler.ListFilesMetadataHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var res map[string][]datasources.FileMetadata
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

	validDsURN := mustURN("urn:data-source:1")
	req := httptest.NewRequest(http.MethodGet, "/v1/data-sources/urn:data-source:1/files/base64-encoded-path/content", nil)
	req.SetPathValue("id", validDsURN.String())
	req.SetPathValue("base64Path", "base64-encoded-path")
	w := httptest.NewRecorder()

	apiHandler.GetFileContentHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var res map[string]string
	json.NewDecoder(w.Body).Decode(&res)
	assert.Equal(t, "package main\n\nfunc main() {}", res["content"])
}

func TestGetFileContentHandler_NotFound(t *testing.T) {
	logger := newTestLogger()
	storeMock := &mockStore{
		err: errors.New("firestore: document not found"),
	}
	apiHandler := &api.API{Fetcher: &mockFetcher{}, Store: storeMock, Logger: logger}

	validDsURN := mustURN("urn:data-source:1")
	req := httptest.NewRequest(http.MethodGet, "/v1/data-sources/urn:data-source:1/files/bad-path/content", nil)
	req.SetPathValue("id", validDsURN.String())
	req.SetPathValue("base64Path", "bad-path")
	w := httptest.NewRecorder()

	apiHandler.GetFileContentHandler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var res map[string]string
	json.NewDecoder(w.Body).Decode(&res)
	assert.Contains(t, res["error"], "File not found")
}
