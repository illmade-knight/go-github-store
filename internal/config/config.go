package config

import (
	"log/slog"
	"os"

	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

// Config defines the single, authoritative configuration for the Sync Service.
type Config struct {
	// Fields that will eventually be mapped from YAML
	RunMode            string
	HTTPListenAddr     string
	FirebaseProjectID  string
	IdentityServiceURL string

	// CorsConfig is the processed, ready-to-use middleware config.
	CorsConfig middleware.CorsConfig

	// GitHubToken is populated exclusively from the "GITHUB_TOKEN" env var.
	GitHubToken string
}

// UpdateConfigWithEnvOverrides takes the base configuration and completes it
// by applying environment variables (like secrets and dynamic ports).
func UpdateConfigWithEnvOverrides(cfg *Config, logger *slog.Logger) (*Config, error) {
	logger.Debug("Applying environment variable overrides...")

	if idURL := os.Getenv("IDENTITY_SERVICE_URL"); idURL != "" {
		logger.Debug("Overriding config value", "key", "IDENTITY_SERVICE_URL", "source", "env")
		cfg.IdentityServiceURL = idURL
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		logger.Debug("Loaded config value", "key", "GITHUB_TOKEN", "source", "env")
		cfg.GitHubToken = token
	}

	if port := os.Getenv("PORT"); port != "" {
		logger.Debug("Overriding config value", "key", "PORT", "source", "env")
		cfg.HTTPListenAddr = ":" + port
	}

	if projectID := os.Getenv("FIREBASE_PROJECT_ID"); projectID != "" {
		logger.Debug("Overriding config value", "key", "FIREBASE_PROJECT_ID", "source", "env")
		cfg.FirebaseProjectID = projectID
	}

	return cfg, nil
}
