package config

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type OptionalConfig struct {
	Pricing OptionalPricingConfig `yaml:"pricing"`
}

type OptionalPricingConfig struct {
	ResponseCosts map[string]billing.ResponseCostRule `yaml:"response_costs"`
}

func (c Config) ReadOptionalConfig() (OptionalConfig, error) {
	path := cmp.Or(c.OptionalConfigFile, c.defaultOptionalConfigFile)
	if path == "" {
		return OptionalConfig{}, nil
	}
	var cfg OptionalConfig
	if err := readYAMLConfig(path, &cfg); err != nil {
		if c.OptionalConfigFile == "" && errors.Is(err, os.ErrNotExist) {
			return OptionalConfig{}, nil
		}
		return OptionalConfig{}, err
	}
	for slug, rule := range cfg.Pricing.ResponseCosts {
		if slug == "" || strings.Contains(slug, "/") {
			return OptionalConfig{}, fmt.Errorf("可选配置 %q：响应费用必须使用供应商 slug", path)
		}
		if err := rule.Validate(); err != nil {
			return OptionalConfig{}, fmt.Errorf("可选配置 %q：响应费用 %s: %w", path, slug, err)
		}
	}
	return cfg, nil
}
