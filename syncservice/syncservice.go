package syncservice

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/tinywideclouds/go-github-store/internal/api"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-github-store/syncservice/config"
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
	dataSourceDB store.DataSourceStore,
	profileDB store.ProfileStore,
	dataGroupDB store.DataGroupStore,
	authMiddleware func(http.Handler) http.Handler,
	logger *slog.Logger,
) *Wrapper {
	baseServer := microservice.NewBaseServer(logger, cfg.HTTPListenAddr)

	apiHandler := &api.API{
		Fetcher:      fetcher,
		DataSourceDB: dataSourceDB,
		ProfileDB:    profileDB,
		DataGroupDB:  dataGroupDB,
		Logger:       logger.With("component", "API"),
	}

	mux := baseServer.Mux()
	corsLogger := logger.With("component", "CORS Middleware")
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CorsConfig, corsLogger)

	optionsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// --- DATA SOURCE & PROFILE OPTIONS ---
	mux.Handle("OPTIONS /v1/data/source", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/source/{id}/sync", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/source/{id}/files", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/source/{id}/profiles", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/source/{id}/profiles/{profileId}", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/source/{id}/files/{base64Path}/content", corsMiddleware(optionsHandler))

	// --- DATAGROUP OPTIONS ---
	mux.Handle("OPTIONS /v1/data/groups", corsMiddleware(optionsHandler))
	mux.Handle("OPTIONS /v1/data/groups/{id}", corsMiddleware(optionsHandler))

	// --- DATA SOURCE ENDPOINTS ---
	listDataSourcesEndpoint := http.HandlerFunc(apiHandler.ListDataSourcesHandler)
	mux.Handle("GET /v1/data/sources", corsMiddleware(authMiddleware(listDataSourcesEndpoint)))

	createDataSourceEndpoint := http.HandlerFunc(apiHandler.CreateDataSourceHandler)
	mux.Handle("POST /v1/data/sources", corsMiddleware(authMiddleware(createDataSourceEndpoint)))

	syncEndpoint := http.HandlerFunc(apiHandler.SyncHandler)
	mux.Handle("POST /v1/data/sources/{id}/sync", corsMiddleware(authMiddleware(syncEndpoint)))

	listFilesEndpoint := http.HandlerFunc(apiHandler.ListFilesMetadataHandler)
	mux.Handle("GET /v1/data/sources/{id}/files", corsMiddleware(authMiddleware(listFilesEndpoint)))

	getFileContentEndpoint := http.HandlerFunc(apiHandler.GetFileContentHandler)
	mux.Handle("GET /v1/data/sources/{id}/files/{base64Path}/content", corsMiddleware(authMiddleware(getFileContentEndpoint)))

	// --- PROFILE ENDPOINTS ---
	listProfilesEndpoint := http.HandlerFunc(apiHandler.ListProfilesHandler)
	mux.Handle("GET /v1/data/sources/{id}/profiles", corsMiddleware(authMiddleware(listProfilesEndpoint)))

	createProfileEndpoint := http.HandlerFunc(apiHandler.CreateProfileHandler)
	mux.Handle("POST /v1/data/sources/{id}/profiles", corsMiddleware(authMiddleware(createProfileEndpoint)))

	updateProfileEndpoint := http.HandlerFunc(apiHandler.UpdateProfileHandler)
	mux.Handle("PUT /v1/data/sources/{id}/profiles/{profileId}", corsMiddleware(authMiddleware(updateProfileEndpoint)))

	deleteProfileEndpoint := http.HandlerFunc(apiHandler.DeleteProfileHandler)
	mux.Handle("DELETE /v1/data/sources/{id}/profiles/{profileId}", corsMiddleware(authMiddleware(deleteProfileEndpoint)))

	// --- DATA GROUP ENDPOINTS ---
	listDataGroupsEndpoint := http.HandlerFunc(apiHandler.ListDataGroupsHandler)
	mux.Handle("GET /v1/data/groups", corsMiddleware(authMiddleware(listDataGroupsEndpoint)))

	createDataGroupEndpoint := http.HandlerFunc(apiHandler.CreateDataGroupHandler)
	mux.Handle("POST /v1/data/groups", corsMiddleware(authMiddleware(createDataGroupEndpoint)))

	updateDataGroupEndpoint := http.HandlerFunc(apiHandler.UpdateDataGroupHandler)
	mux.Handle("PUT /v1/data/groups/{id}", corsMiddleware(authMiddleware(updateDataGroupEndpoint)))

	deleteDataGroupEndpoint := http.HandlerFunc(apiHandler.DeleteDataGroupHandler)
	mux.Handle("DELETE /v1/data/groups/{id}", corsMiddleware(authMiddleware(deleteDataGroupEndpoint)))

	return &Wrapper{
		BaseServer: baseServer,
		logger:     logger,
	}
}

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
		w.logger.Info("Data Sources Service is ready to accept requests")
		return nil
	}
}
