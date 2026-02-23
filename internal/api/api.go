package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
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

// SyncHandler handles the POST /v1/caches/sync request.
func (a *API) SyncHandler(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.Logger.Warn("Invalid JSON body received", "error", err)
		response.WriteJSONError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	// 1. Validate Input
	if req.Repo == "" || req.Branch == "" {
		a.Logger.Warn("Missing required fields", "repo", req.Repo, "branch", req.Branch)
		response.WriteJSONError(w, http.StatusBadRequest, "Both 'repo' and 'branch' are required fields")
		return
	}

	// 2. Resolve Cache ID (Create new if not provided)
	cacheID := req.CacheID
	if cacheID == "" {
		cacheID = "urn:llm:cache:" + uuid.New().String()
		a.Logger.Info("No cacheId provided, generated new one", "cacheId", cacheID)
	}

	// 3. Fetch from GitHub
	files, err := a.Fetcher.FetchRepository(r.Context(), req.Repo, req.Branch)
	if err != nil {
		a.Logger.Error("Failed to fetch repository", "repo", req.Repo, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to fetch repository from GitHub")
		return
	}

	// 4. Save to Firestore
	if err := a.Store.SaveSync(r.Context(), cacheID, req.Repo, req.Branch, files); err != nil {
		a.Logger.Error("Failed to save sync to database", "cacheId", cacheID, "error", err)
		response.WriteJSONError(w, http.StatusInternalServerError, "Failed to persist repository data")
		return
	}

	// 5. Respond Success
	res := SyncResponse{
		CacheID:        cacheID,
		Status:         "success",
		FilesProcessed: len(files),
	}
	response.WriteJSON(w, http.StatusOK, res)
}
