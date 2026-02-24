package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tinywideclouds/go-github-store/internal/filter"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/response"
)

// API holds the injected domain dependencies and handles HTTP requests.
type API struct {
	Fetcher github.Fetcher
	Store   store.Store
	Logger  *slog.Logger
}

type CreateCacheRequest struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"` // Optional, will default to main/master via Fetcher
}

type SyncRequest struct {
	IngestionRules filter.FilterRules `json:"ingestionRules"`
}

type SyncResponse struct {
	CacheID        string `json:"cacheId"`
	Status         string `json:"status"`
	FilesProcessed int    `json:"filesProcessed"`
}

type ProfileRequest struct {
	Name      string `json:"name"`
	RulesYaml string `json:"rulesYaml"`
}

func extractOwnerRepo(input string) string {
	// Clean up whitespace
	clean := strings.TrimSpace(input)

	// Strip protocols and domain
	clean = strings.TrimPrefix(clean, "https://github.com/")
	clean = strings.TrimPrefix(clean, "http://github.com/")
	clean = strings.TrimPrefix(clean, "github.com/")

	// Strip trailing git extensions or slashes
	clean = strings.TrimSuffix(clean, ".git")
	clean = strings.TrimRight(clean, "/")

	return clean
}

// CreateCacheHandler handles POST /v1/caches
// It fetches the repository metadata, creates a skeleton in Firestore, and returns the analysis.
func (a *API) CreateCacheHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateCacheRequest
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

	// 1. Analyze the repository via GitHub Git Trees API
	analysis, err := a.Fetcher.AnalyzeRepository(r.Context(), cleanRepo, req.Branch)
	if err != nil {
		a.Logger.Error("Failed to analyze repository", "repo", cleanRepo, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to analyze repository from GitHub")
		return
	}

	// 2. Prepare the Cache Skeleton
	cacheID := "urn:llm:cache:" + uuid.New().String()
	meta := &store.CacheMetadata{
		ID:              cacheID,
		Repo:            analysis.Repo,
		Branch:          analysis.Branch,
		SyncedCommitSHA: analysis.CommitSHA,
		Status:          "unsynced",
		Analysis: store.CacheAnalysis{
			TotalFiles:     analysis.TotalFiles,
			TotalSizeBytes: analysis.TotalSizeBytes,
			Extensions:     analysis.Extensions,
		},
	}

	// 3. Save to Firestore
	if err := a.Store.CreateCache(r.Context(), meta); err != nil {
		a.Logger.Error("Failed to create cache skeleton", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to persist cache skeleton")
		return
	}

	response.WriteJSON(w, http.StatusCreated, meta)
}

// SyncHandler handles POST /v1/caches/{id}/sync
// It pulls the skeleton, applies ingestion filters, downloads files, and updates Firestore.
func (a *API) SyncHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	if cacheID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID")
		return
	}

	var req SyncRequest
	// We ignore EOF errors here as the body might intentionally be empty if they want to sync everything
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	// 1. Fetch the existing Cache Skeleton
	cache, err := a.Store.GetCache(r.Context(), cacheID)
	if err != nil {
		a.Logger.Warn("Cache not found for sync", "cacheId", cacheID)
		response.WriteJSONError(w, http.StatusNotFound, "Cache not found")
		return
	}

	if cache.Status == "syncing" {
		response.WriteJSONError(w, http.StatusConflict, "Cache is already currently syncing")
		return
	}

	// 2. Fetch from GitHub with Ingestion Rules applied
	files, err := a.Fetcher.FetchRepository(r.Context(), cache.Repo, cache.Branch, &req.IngestionRules)
	if err != nil {
		a.Logger.Error("Failed to fetch repository contents", "repo", cache.Repo, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to download repository contents from GitHub")
		return
	}

	// 3. Save files and updated metadata to Firestore
	if err := a.Store.SaveSync(r.Context(), cacheID, cache.Repo, cache.Branch, cache.SyncedCommitSHA, files); err != nil {
		a.Logger.Error("Failed to save sync to database", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to persist repository data")
		return
	}

	res := SyncResponse{
		CacheID:        cacheID,
		Status:         "success",
		FilesProcessed: len(files),
	}
	response.WriteJSON(w, http.StatusOK, res)
}

// ListCachesHandler handles GET /v1/caches
func (a *API) ListCachesHandler(w http.ResponseWriter, r *http.Request) {
	caches, err := a.Store.ListCaches(r.Context())
	if err != nil {
		a.Logger.Error("Failed to list caches", "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve caches")
		return
	}
	if caches == nil {
		caches = []store.CacheMetadata{} // Ensure we return [] instead of null
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"caches": caches})
}

// ListFilesMetadataHandler handles GET /v1/caches/{id}/files
func (a *API) ListFilesMetadataHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	if cacheID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID")
		return
	}

	files, err := a.Store.ListFilesMetadata(r.Context(), cacheID)
	if err != nil {
		a.Logger.Error("Failed to list file metadata", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve file metadata")
		return
	}
	if files == nil {
		files = []store.FileMetadata{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"files": files})
}

// ListProfilesHandler handles GET /v1/caches/{id}/profiles
func (a *API) ListProfilesHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	if cacheID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID")
		return
	}

	profiles, err := a.Store.ListProfiles(r.Context(), cacheID)
	if err != nil {
		a.Logger.Error("Failed to list profiles", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to retrieve profiles")
		return
	}
	if profiles == nil {
		profiles = []store.Profile{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"profiles": profiles})
}

// CreateProfileHandler handles POST /v1/caches/{id}/profiles
func (a *API) CreateProfileHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	if cacheID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID")
		return
	}

	var req ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Name == "" || req.RulesYaml == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Name and rulesYaml are required")
		return
	}

	// Use our new isolated filter package for YAML validation
	if _, err := filter.ParseYAML(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	profile := &store.Profile{
		ID:        "urn:llm:profile:" + uuid.New().String(),
		Name:      req.Name,
		RulesYaml: req.RulesYaml,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := a.Store.CreateProfile(r.Context(), cacheID, profile); err != nil {
		a.Logger.Error("Failed to create profile", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to create profile")
		return
	}

	response.WriteJSON(w, http.StatusCreated, profile)
}

// UpdateProfileHandler handles PUT /v1/caches/{id}/profiles/{profileId}
func (a *API) UpdateProfileHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	profileID := r.PathValue("profileId")
	if cacheID == "" || profileID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID or profile ID")
		return
	}

	var req ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Name == "" || req.RulesYaml == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Name and rulesYaml are required")
		return
	}

	if _, err := filter.ParseYAML(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	profile := &store.Profile{
		ID:        profileID,
		Name:      req.Name,
		RulesYaml: req.RulesYaml,
		UpdatedAt: time.Now(),
	}

	if err := a.Store.UpdateProfile(r.Context(), cacheID, profile); err != nil {
		a.Logger.Error("Failed to update profile", "profileId", profileID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to update profile")
		return
	}

	response.WriteJSON(w, http.StatusOK, profile)
}

// DeleteProfileHandler handles DELETE /v1/caches/{id}/profiles/{profileId}
func (a *API) DeleteProfileHandler(w http.ResponseWriter, r *http.Request) {
	cacheID := r.PathValue("id")
	profileID := r.PathValue("profileId")
	if cacheID == "" || profileID == "" {
		response.WriteJSONError(w, http.StatusBadRequest, "Missing cache ID or profile ID")
		return
	}

	if err := a.Store.DeleteProfile(r.Context(), cacheID, profileID); err != nil {
		a.Logger.Error("Failed to delete profile", "profileId", profileID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to delete profile")
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
