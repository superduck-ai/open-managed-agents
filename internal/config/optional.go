package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type OptionalConfig struct {
	Pricing OptionalPricingConfig `yaml:"pricing"`
}

type OptionalPricingConfig struct {
	ResponseCosts map[string]billing.ResponseCostRule `yaml:"response_costs"`
}

func ReadOptionalConfig(path string) (OptionalConfig, error) {
	if path == "" {
		return OptionalConfig{}, nil
	}
	var cfg OptionalConfig
	if err := readYAMLConfig(path, &cfg); err != nil {
		return OptionalConfig{}, err
	}
	for slug, rule := range cfg.Pricing.ResponseCosts {
		if slug == "" || strings.Contains(slug, "/") {
			return OptionalConfig{}, errors.New("响应费用必须使用供应商 slug")
		}
		if err := rule.Validate(); err != nil {
			return OptionalConfig{}, fmt.Errorf("响应费用 %s: %w", slug, err)
		}
	}
	return cfg, nil
}
