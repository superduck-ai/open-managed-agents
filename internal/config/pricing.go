package config

import (
	"errors"
	"strings"
	"time"
)

type PricingConfig struct {
	SnapshotFile    string        `yaml:"snapshot_file"`
	ReadOnly        bool          `yaml:"read_only"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	ReloadInterval  time.Duration `yaml:"reload_interval"`
}

func validatePricing(cfg PricingConfig) error {
	if strings.TrimSpace(cfg.SnapshotFile) == "" || cfg.RefreshInterval <= 0 || cfg.ReloadInterval <= 0 {
		return errors.New("pricing requires a snapshot_file and positive refresh_interval and reload_interval")
	}
	return nil
}
