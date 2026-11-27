package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOptionalConfigAtDefaultLocation(t *testing.T) {
	mainPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestYAML(t, mainPath, "")
	t.Setenv(configFileEnv, mainPath)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if optional, err := cfg.ReadOptionalConfig(); err != nil || len(optional.Pricing.ResponseCosts) != 0 {
		t.Fatalf("默认文件缺失时应使用空配置：%+v, %v", optional, err)
	}
	runtimeDir := filepath.Join(filepath.Dir(mainPath), "runtime")
	if err := os.Mkdir(runtimeDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeConfigTestContents(t, filepath.Join(runtimeDir, "optional.yaml"), `pricing:
  response_costs:
    example:
      currency: CNY
      exchange_rate: "7.2"
      protocols:
        messages:
          non_streaming:
            amount_pointer: /receipt/amount
`)
	optional, err := cfg.ReadOptionalConfig()
	if err != nil {
		t.Fatal(err)
	}
	rule := optional.Pricing.ResponseCosts["example"]
	if cost := rule.ExtractFor("messages", false, []byte(`{"receipt":{"amount":"7.2"}}`)); cost == nil || cost.USDCents != 100 {
		t.Fatalf("默认文件中的规则未加载：%+v", cost)
	}
	cfg.OptionalConfigFile = filepath.Join(runtimeDir, "override.yaml")
	if optional, err := cfg.ReadOptionalConfig(); !errors.Is(err, os.ErrNotExist) || len(optional.Pricing.ResponseCosts) != 0 {
		t.Fatalf("显式路径应优先，缺失时返回读取错误：%+v, %v", optional, err)
	}
}
