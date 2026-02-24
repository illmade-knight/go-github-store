package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

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

type SyncRequest struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	CacheID string `json:"cacheId"` // Optional
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

// validateYamlRules ensures the provided string maps correctly to our FilterRules struct.
func validateYamlRules(yamlStr string) error {
	var rules store.FilterRules
	if err := yaml.Unmarshal([]byte(yamlStr), &rules); err != nil {
		return fmt.Errorf("invalid YAML structure: %w", err)
	}
	return nil
}

// SyncHandler handles the POST /v1/caches/sync request.
func (a *API) SyncHandler(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.Logger.Warn("Invalid JSON body received", "error", err)
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	if req.Repo == "" || req.Branch == "" {
		a.Logger.Warn("Missing required fields", "repo", req.Repo, "branch", req.Branch)
		response.WriteJSONError(w, http.StatusBadRequest, "Both 'repo' and 'branch' are required fields")
		return
	}

	cacheID := req.CacheID
	if cacheID == "" {
		cacheID = "urn:llm:cache:" + uuid.New().String()
		a.Logger.Info("No cacheId provided, generated new one", "cacheId", cacheID)
	}

	files, err := a.Fetcher.FetchRepository(r.Context(), req.Repo, req.Branch)
	if err != nil {
		a.Logger.Error("Failed to fetch repository", "repo", req.Repo, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to fetch repository from GitHub")
		return
	}

	if err := a.Store.SaveSync(r.Context(), cacheID, req.Repo, req.Branch, files); err != nil {
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

	if err := validateYamlRules(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid YAML format: "+err.Error())
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

	if err := validateYamlRules(req.RulesYaml); err != nil {
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid YAML format: "+err.Error())
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
