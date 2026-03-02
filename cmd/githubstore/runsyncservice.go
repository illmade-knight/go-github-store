package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"gopkg.in/yaml.v3"

	"github.com/tinywideclouds/go-github-store/internal/config"
	"github.com/tinywideclouds/go-github-store/internal/github"
	"github.com/tinywideclouds/go-github-store/internal/store"
	"github.com/tinywideclouds/go-github-store/syncservice"

	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

//go:embed local.yaml
var configFile []byte

//go:embed github_ignore.yaml
var ignoreFile []byte

func main() {
	// 1. Setup Structured Logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load Base Configuration
	var yamlCfg config.YamlConfig
	if err := yaml.Unmarshal(configFile, &yamlCfg); err != nil {
		logger.Error("Failed to parse local.yaml", "error", err)
		os.Exit(1)
	}

	baseCfg, err := config.NewConfigFromYaml(&yamlCfg, logger)
	if err != nil {
		logger.Error("Failed to map base configuration", "error", err)
		os.Exit(1)
	}

	// 3. Apply Environment Overrides (e.g. GITHUB_TOKEN, PORT)
	cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)
	if err != nil {
		logger.Error("Failed to apply environment overrides", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()

	firestoreStore, githubFetcher, err := newDependencies(ctx, cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize dependencies", "error", err)
		os.Exit(1)
	}
	defer firestoreStore.Close()

	// 3b. Authentication Middleware
	authMiddleware, err := newAuthMiddleware(cfg, logger)
	if err != nil {
		logger.Error("Failed to initialize authentication middleware", "err", err)
		os.Exit(1)
	}

	// 6. Initialize and Start the Service
	serviceWrapper := syncservice.NewSyncService(cfg, githubFetcher, firestoreStore, authMiddleware, logger)

	// --- 5. Start Service and Handle Shutdown ---
	errChan := make(chan error, 1)
	go func() {
		logger.Info("Starting service...", "address", cfg.HTTPListenAddr)
		if startErr := serviceWrapper.Start(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			errChan <- startErr
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		logger.Error("Service failed", "err", err)
		os.Exit(1)
	case sig := <-quit:
		logger.Info("OS signal received, initiating shutdown.", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if shutdownErr := serviceWrapper.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("Service shutdown failed", "err", shutdownErr)
		} else {
			logger.Info("Service shutdown complete")
		}
	}
}

// newDependencies builds the GenAI client.
func newDependencies(ctx context.Context, cfg *config.Config, logger *slog.Logger) (store.Store, *github.Client, error) {
	// 5. Initialize Infrastructure Clients
	// Firestore relies on GOOGLE_APPLICATION_CREDENTIALS being present in the environment
	fsClient, err := firestore.NewClient(ctx, cfg.GoogleProjectID)
	if err != nil {
		logger.Error("Failed to initialize Firestore client", "error", err)
		os.Exit(1)
	}

	firestoreStore := store.NewFirestoreClient(fsClient, cfg.StoreCollections, logger.With("component", "Firestore"))

	// 4. Load GitHub Ignore Rules
	ignoreCfg, err := config.LoadGitHubIgnoreConfig(ignoreFile)
	if err != nil {
		logger.Error("Failed to load github ignore configuration", "error", err)
		os.Exit(1)
	}
	githubFetcher := github.NewClient(cfg.GitHubToken, ignoreCfg, logger.With("component", "GitHub"))

	return firestoreStore, githubFetcher, nil
}

// newAuthMiddleware creates the JWT-validating middleware, or a bypass if in DEV mode.
func newAuthMiddleware(cfg *config.Config, logger *slog.Logger) (func(http.Handler) http.Handler, error) {
	if os.Getenv("DISABLE_AUTH") == "true" {
		logger.Warn("⚠️  AUTH DISABLED: Running in development mode with NoopAuth")
		return middleware.NoopAuth(true, "dev-user-123"), nil
	}

	sanitizedIdentityURL := strings.Trim(cfg.IdentityServiceURL, "\"")
	logger.Debug("Discovering JWT config", "identity_url", sanitizedIdentityURL)

	jwksURL, err := middleware.DiscoverAndValidateJWTConfig(sanitizedIdentityURL, middleware.RSA256, logger)
	if err != nil {
		logger.Warn("JWT configuration validation failed. This may be fatal if auth is required.", "err", err)
	} else {
		logger.Info("VERIFIED JWKS CONFIG")
	}

	authMiddleware, err := middleware.NewJWKSAuthMiddleware(jwksURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth middleware: %w", err)
	}

	logger.Debug("JWKS auth middleware created successfully")
	return authMiddleware, nil
}
