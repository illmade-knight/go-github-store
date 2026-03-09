package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	datasources "github.com/tinywideclouds/go-data-sources/pkg/v1"
	"github.com/tinywideclouds/go-data-sources/pkg/yaml"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/response"

	urn "github.com/tinywideclouds/go-platform/pkg/net/v1"
)

type API struct {
	Fetcher github.Fetcher
	Store   store.Store
	Logger  *slog.Logger
}

func extractOwnerRepo(input string) string {
	clean := strings.TrimSpace(input)
	clean = strings.TrimPrefix(clean, "https://github.com/")
	clean = strings.TrimPrefix(clean, "http://github.com/")
	clean = strings.TrimPrefix(clean, "github.com/")
	clean = strings.TrimSuffix(clean, ".git")
	clean = strings.TrimRight(clean, "/")
	return clean
}

func (a *API) CreateDataSourceHandler(w http.ResponseWriter, r *http.Request) {
	var req datasources.CreateDataSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.Logger.Warn("Invalid JSON body received", "error", err)
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Repo == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "'repo' is a required field")
		return
	}

	cleanRepo := extractOwnerRepo(req.Repo)

	analysis, err := a.Fetcher.AnalyzeRepository(r.Context(), cleanRepo, req.Branch)
	if err != nil {
		a.Logger.Error("Failed to analyze repository", "repo", cleanRepo, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to analyze repository from GitHub")
		return
	}

	dsIDStr := "urn:data-source:" + uuid.New().String()

	meta := &datasources.DataSourceMetadata{
		ID:              dsIDStr,
		Repo:            analysis.Repo,
		Branch:          analysis.Branch,
		SyncedCommitSha: analysis.CommitSHA,
		Status:          "unsynced",
		FileCount:       0,
		Analysis: &datasources.DataSourceAnalysis{
			TotalFiles:     int32(analysis.TotalFiles),
			TotalSizeBytes: int32(analysis.TotalSizeBytes),
			Extensions: func() map[string]int32 {
				m := make(map[string]int32)
				for k, v := range analysis.Extensions {
					m[k] = int32(v)
				}
				return m
			}(),
		},
	}

	if err := a.Store.CreateDataSource(r.Context(), meta); err != nil {
		a.Logger.Error("Failed to create data source skeleton", "dsID", dsIDStr, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to persist data source skeleton")
		return
	}

	response.WriteJSON(w, http.StatusCreated, meta)
}

func (a *API) SyncHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	var req datasources.SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	meta, err := a.Store.GetDataSource(r.Context(), dsID)
	if err != nil {
		a.Logger.Warn("Data Source not found for sync", "dsID", dsID.String())
		response.WriteJSONError(w, http.StatusNotFound, "Data Source not found")
		return
	}

	if meta.Status == "syncing" {
		response.WriteJSONError(w, http.StatusConflict, "Data Source is already currently syncing")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteJSONError(w, http.StatusInternalServerError, "Streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var streamMu sync.Mutex
	sendEvent := func(stage string, details map[string]any) {
		streamMu.Lock()
		defer streamMu.Unlock()

		payload := map[string]any{"stage": stage, "details": details}
		bytes, _ := json.Marshal(payload)

		fmt.Fprintf(w, "data: %s\n\n", string(bytes))
		flusher.Flush()
	}

	sendEvent("init", map[string]any{"message": "Starting sync process..."})

	var domainRules *yaml.FilterRules
	if req.IngestionRules != nil {
		domainRules = &yaml.FilterRules{
			Include: req.IngestionRules.Include,
			Exclude: req.IngestionRules.Exclude,
		}
	}

	files, fetchErr := a.Fetcher.FetchRepository(r.Context(), meta.Repo, meta.Branch, domainRules, sendEvent)
	if fetchErr != nil {
		sendEvent("error", map[string]any{"message": "Failed to fetch from GitHub"})
		return
	}

	if dbErr := a.Store.SaveSync(r.Context(), dsID, meta.Repo, meta.Branch, meta.SyncedCommitSha, files, sendEvent); dbErr != nil {
		sendEvent("error", map[string]any{"message": "Failed to save to Firestore"})
		return
	}

	sendEvent("complete", map[string]any{"filesProcessed": len(files)})
}

func (a *API) ListDataSourcesHandler(w http.ResponseWriter, r *http.Request) {
	sources, err := a.Store.ListDataSources(r.Context())
	if err != nil {
		a.Logger.Error("Failed to list data sources", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve data sources")
		return
	}
	if sources == nil {
		sources = []datasources.DataSourceMetadata{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"dataSources": sources})
}

func (a *API) ListFilesMetadataHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	files, err := a.Store.ListFilesMetadata(r.Context(), dsID)
	if err != nil {
		a.Logger.Error("Failed to list file metadata", "dsID", dsID.String(), "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve file metadata")
		return
	}
	if files == nil {
		files = []datasources.FileMetadata{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

func (a *API) ListProfilesHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	profiles, err := a.Store.ListProfiles(r.Context(), dsID)
	if err != nil {
		a.Logger.Error("Failed to list profiles", "dsID", dsID.String(), "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve profiles")
		return
	}
	if profiles == nil {
		profiles = []datasources.Profile{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"profiles": profiles})
}

func (a *API) CreateProfileHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	var req datasources.ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Name == "" || req.RulesYaml == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Name and rulesYaml are required")
		return
	}

	if _, err := yaml.ParseYAML(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	profileIDStr := "urn:profile:" + uuid.New().String()

	profile := &datasources.Profile{
		ID:        profileIDStr,
		Name:      req.Name,
		RulesYaml: req.RulesYaml,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := a.Store.CreateProfile(r.Context(), dsID, profile); err != nil {
		a.Logger.Error("Failed to create profile", "dsID", dsID.String(), "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to create profile")
		return
	}

	response.WriteJSON(w, http.StatusCreated, profile)
}

func (a *API) UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	profileID, err := urn.Parse(r.PathValue("profileId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid profile ID format")
		return
	}

	var req datasources.ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Name == "" || req.RulesYaml == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Name and rulesYaml are required")
		return
	}

	if _, err := yaml.ParseYAML(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	profile := &datasources.Profile{
		ID:        profileID.String(),
		Name:      req.Name,
		RulesYaml: req.RulesYaml,
		UpdatedAt: time.Now(),
	}

	if err := a.Store.UpdateProfile(r.Context(), dsID, profile); err != nil {
		a.Logger.Error("Failed to update profile", "profileId", profileID.String(), "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to update profile")
		return
	}

	response.WriteJSON(w, http.StatusOK, profile)
}

func (a *API) DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	profileID, err := urn.Parse(r.PathValue("profileId"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid profile ID format")
		return
	}

	if err := a.Store.DeleteProfile(r.Context(), dsID, profileID); err != nil {
		a.Logger.Error("Failed to delete profile", "profileId", profileID.String(), "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to delete profile")
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) GetFileContentHandler(w http.ResponseWriter, r *http.Request) {
	dsID, err := urn.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid data source ID format")
		return
	}

	base64Path := r.PathValue("base64Path")
	if base64Path == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing base64 file path")
		return
	}

	content, err := a.Store.GetFileContent(r.Context(), dsID, base64Path)
	if err != nil {
		a.Logger.Error("Failed to fetch file content", "dsID", dsID.String(), "docId", base64Path, "error", err)
		response.WriteJSONError(w, http.StatusNotFound, "File not found or unreadable")
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"content": content,
	})
}
