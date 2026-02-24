// internal/config/config_test.go
package config_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-github-store/internal/config"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBaseConfig() *config.Config {
	return &config.Config{
		RunMode:         "base-mode",
		HTTPListenAddr:  ":8080",
		GoogleProjectID: "base-project",
	}
}

func TestUpdateConfigWithEnvOverrides(t *testing.T) {
	logger := newTestLogger()

	t.Run("Success - All overrides applied", func(t *testing.T) {
		baseCfg := newBaseConfig()

		t.Setenv("GITHUB_TOKEN", "ghp_secret_token")
		t.Setenv("PORT", "9090")
		t.Setenv("FIREBASE_PROJECT_ID", "override-project")

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		// Check overridden fields
		assert.Equal(t, "ghp_secret_token", cfg.GitHubToken)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, "override-project", cfg.GoogleProjectID)

		// Non-overridden fields should remain unchanged
		assert.Equal(t, "base-mode", cfg.RunMode)
	})

	t.Run("Success - No overrides applied if env vars are empty", func(t *testing.T) {
		baseCfg := newBaseConfig()

		cfg, err := config.UpdateConfigWithEnvOverrides(baseCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "", cfg.GitHubToken)
		assert.Equal(t, ":8080", cfg.HTTPListenAddr)
		assert.Equal(t, "base-project", cfg.GoogleProjectID)
	})
}
