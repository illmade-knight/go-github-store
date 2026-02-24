package syncservice

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/tinywideclouds/go-github-store/internal/api"
	"github.com/tinywideclouds/go-github-store/internal/config"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-microservice-base/pkg/microservice"
	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

type Wrapper struct {
	*microservice.BaseServer
	logger *slog.Logger
}

// NewSyncService creates and wires up the GitHub Ingestion Service.
func NewSyncService(
	cfg *config.Config,
	fetcher github.Fetcher,
	dbStore store.Store,
	authMiddleware func(http.Handler) http.Handler,
	logger *slog.Logger,
) *Wrapper {
	// 1. Create the standard base server
	baseServer := microservice.NewBaseServer(logger, cfg.HTTPListenAddr)

	// 2. Initialize API Layer
	apiHandler := &api.API{
		Fetcher: fetcher,
		Store:   dbStore,
		Logger:  logger.With("component", "API"),
	}

	// 3. Setup Routing & Middleware
	mux := baseServer.Mux()
	corsLogger := logger.With("component", "CORS Middleware")
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CorsConfig, corsLogger)

	// 4. Register OPTIONS for CORS pre-flight across all routes
	optionsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	mux.Handle("OPTIONS /v1/caches", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/caches/{id}/sync", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/caches/{id}/files", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/caches/{id}/profiles", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/caches/{id}/profiles/{profileId}", corsMiddleware(optionsHandler))

	// 5. Register API Routes with CORS and Auth wrappers

	// Sync Execution
	syncEndpoint := http.HandlerFunc(apiHandler.SyncHandler)
	mux.Handle("POST /v1/caches/{id}/sync", corsMiddleware(authMiddleware(syncEndpoint)))

	// Cache & File Reading
	listCachesEndpoint := http.HandlerFunc(apiHandler.ListCachesHandler)
	mux.Handle("GET /v1/caches", corsMiddleware(authMiddleware(listCachesEndpoint)))

	createCacheEndpoint := http.HandlerFunc(apiHandler.CreateCacheHandler)
	mux.Handle("POST /v1/caches", corsMiddleware(authMiddleware(createCacheEndpoint)))

	listFilesEndpoint := http.HandlerFunc(apiHandler.ListFilesMetadataHandler)
	mux.Handle("GET /v1/caches/{id}/files", corsMiddleware(authMiddleware(listFilesEndpoint)))

	// Profile CRUD Management
	listProfilesEndpoint := http.HandlerFunc(apiHandler.ListProfilesHandler)
	mux.Handle("GET /v1/caches/{id}/profiles", corsMiddleware(authMiddleware(listProfilesEndpoint)))

	createProfileEndpoint := http.HandlerFunc(apiHandler.CreateProfileHandler)
	mux.Handle("POST /v1/caches/{id}/profiles", corsMiddleware(authMiddleware(createProfileEndpoint)))

	updateProfileEndpoint := http.HandlerFunc(apiHandler.UpdateProfileHandler)
	mux.Handle("PUT /v1/caches/{id}/profiles/{profileId}", corsMiddleware(authMiddleware(updateProfileEndpoint)))

	deleteProfileEndpoint := http.HandlerFunc(apiHandler.DeleteProfileHandler)
	mux.Handle("DELETE /v1/caches/{id}/profiles/{profileId}", corsMiddleware(authMiddleware(deleteProfileEndpoint)))

	return &Wrapper{
		BaseServer: baseServer,
		logger:     logger,
	}
}

// Start boots the HTTP server and handles any instant startup failures.
func (w *Wrapper) Start() error {
	errChan := make(chan error, 1)
	httpReadyChan := make(chan struct{})
	w.BaseServer.SetReadyChannel(httpReadyChan)

	go func() {
		if err := w.BaseServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.logger.Error("Server failed to start", "error", err)
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-httpReadyChan:
		w.logger.Info("GitHub Sync Service is ready to accept requests")
		return nil
	}
}
