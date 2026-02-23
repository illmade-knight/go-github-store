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

	syncEndpoint := http.HandlerFunc(apiHandler.SyncHandler)
	mux.Handle("POST /v1/caches/sync", corsMiddleware(authMiddleware(syncEndpoint)))

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
