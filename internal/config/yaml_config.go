package config

import (
	"log/slog"

	"github.com/tinywideclouds/go-microservice-base/pkg/middleware"
)

// YamlConfig is the structure that mirrors the raw local.yaml file.
type YamlConfig struct {
	RunMode            string `yaml:"run_mode"`
	HTTPListenAddr     string `yaml:"http_listen_addr"`
	FirebaseProjectID  string `yaml:"firebase_project_id"`
	IdentityServiceURL string `yaml:"identity_service_url"`
	Cors               struct {
		AllowedOrigins []string `yaml:"allowed_origins"`
		Role           string   `yaml:"cors_role"`
	} `yaml:"cors"`
}

// NewConfigFromYaml converts the YamlConfig into a clean, base Config struct.
// This struct is the "Stage 1" configuration, ready to be augmented by environment overrides.
func NewConfigFromYaml(baseCfg *YamlConfig, logger *slog.Logger) (*Config, error) {
	logger.Debug("Mapping YAML config to base config struct")

	cfg := &Config{
		RunMode:            baseCfg.RunMode,
		HTTPListenAddr:     baseCfg.HTTPListenAddr,
		IdentityServiceURL: baseCfg.IdentityServiceURL,
		FirebaseProjectID:  baseCfg.FirebaseProjectID,
		CorsConfig: middleware.CorsConfig{
			AllowedOrigins: baseCfg.Cors.AllowedOrigins,
			Role:           middleware.CorsRole(baseCfg.Cors.Role),
		},
	}
	// Note: GitHubToken is intentionally left blank here, as it's an environment override exclusively.

	logger.Debug("YAML config mapping complete",
		"run_mode", cfg.RunMode,
		"http_listen_addr", cfg.HTTPListenAddr,
		"identity_service_url", cfg.IdentityServiceURL,
	)

	return cfg, nil
}
