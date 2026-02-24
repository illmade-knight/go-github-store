// internal/config/yaml_config_test.go
package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinywideclouds/go-github-store/internal/config"
)

func TestNewConfigFromYaml(t *testing.T) {
	// Note: newTestLogger() is reused from config_test.go in the same package
	logger := newTestLogger()

	t.Run("Success - maps all fields correctly from YAML struct", func(t *testing.T) {
		yamlCfg := &config.YamlConfig{
			RunMode:         "test-mode",
			HTTPListenAddr:  ":9090",
			GoogleProjectID: "test-google-project",
			Cors: struct {
				AllowedOrigins []string `yaml:"allowed_origins"`
				Role           string   `yaml:"cors_role"`
			}{
				AllowedOrigins: []string{"http://origin1.com", "http://origin2.com"},
				Role:           "my-custom-role",
			},
		}

		cfg, err := config.NewConfigFromYaml(yamlCfg, logger)

		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "test-mode", cfg.RunMode)
		assert.Equal(t, ":9090", cfg.HTTPListenAddr)
		assert.Equal(t, "test-google-project", cfg.GoogleProjectID)
		assert.Equal(t, []string{"http://origin1.com", "http://origin2.com"}, cfg.CorsConfig.AllowedOrigins)
		assert.Equal(t, "my-custom-role", string(cfg.CorsConfig.Role))
	})
}
