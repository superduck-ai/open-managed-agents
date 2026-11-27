package api

import (
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func loadOptionalConfig(path string, logger *slog.Logger) config.OptionalConfig {
	cfg, err := config.ReadOptionalConfig(path)
	if err != nil {
		logger.Warn("可选配置未加载", "path", path, "error", err)
	}
	return cfg
}
