package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
)

type PricingConfig struct {
	ResponseCosts   map[string]billing.ResponseCostRule `yaml:"response_costs"`
	SnapshotFile    string                              `yaml:"snapshot_file"`
	ReadOnly        bool                                `yaml:"read_only"`
	RefreshInterval time.Duration                       `yaml:"refresh_interval"`
	ReloadInterval  time.Duration                       `yaml:"reload_interval"`
}

func validatePricing(cfg PricingConfig) error {
	if strings.TrimSpace(cfg.SnapshotFile) == "" || cfg.RefreshInterval <= 0 || cfg.ReloadInterval <= 0 {
		return errors.New("pricing requires a snapshot_file and positive refresh_interval and reload_interval")
	}
	for slug, rule := range cfg.ResponseCosts {
		if slug == "" || strings.Contains(slug, "/") {
			return errors.New("响应费用必须使用供应商 slug")
		}
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("响应费用 %s: %w", slug, err)
		}
	}
	return nil
}
