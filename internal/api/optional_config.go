package api

import (
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func loadOptionalConfig(configured config.Config, logger *slog.Logger) config.OptionalConfig {
	cfg, err := configured.ReadOptionalConfig()
	if err != nil {
		logger.Warn("可选配置未加载", "error", err)
	}
	return cfg
}
