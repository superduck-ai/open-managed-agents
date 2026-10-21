package devinit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultImagePrefix = "ghcr.io/superduck-ai"

var imagePrefixPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-]+[a-z0-9]+)*(?::[0-9]+)?(?:/[a-z0-9]+(?:[._-]+[a-z0-9]+)*)*$`)

type imagesConfig struct {
	Prefix string `yaml:"prefix"`
}

func loadImagesConfig() (imagesConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return imagesConfig{}, err
	}
	return loadImagesConfigFile(filepath.Join(homeDir, ".oma-dev", "images.yaml"))
}

func loadImagesConfigFile(path string) (imagesConfig, error) {
	cfg := imagesConfig{Prefix: defaultImagePrefix}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return imagesConfig{}, err
	}
	if !info.Mode().IsRegular() {
		return imagesConfig{}, fmt.Errorf("image configuration must be a regular file")
	}
	if err := loadStrictYAML(path, &cfg); err != nil {
		return imagesConfig{}, err
	}
	cfg.Prefix = strings.TrimSpace(cfg.Prefix)
	if cfg.Prefix == "" {
		cfg.Prefix = defaultImagePrefix
	}
	cfg.Prefix = strings.TrimRight(cfg.Prefix, "/")
	if !imagePrefixPattern.MatchString(cfg.Prefix) {
		return imagesConfig{}, fmt.Errorf("images.prefix must be a registry and optional namespace without scheme, credentials or tag")
	}
	return cfg, nil
}

func (cfg imagesConfig) reference(name string) string {
	prefix := cfg.Prefix
	if prefix == "" {
		prefix = defaultImagePrefix
	}
	return prefix + "/" + name + ":latest"
}

func isDefaultSandboxTemplate(template string) bool {
	switch template {
	case "", "managed-agent-sandbox", "oma/managed-agent-sandbox:latest", defaultImagePrefix + "/managed-agent-sandbox:latest":
		return true
	default:
		return false
	}
}

func (cfg imagesConfig) sandboxTemplate(template string) string {
	if isDefaultSandboxTemplate(template) {
		return cfg.reference("managed-agent-sandbox")
	}
	return template
}

func prepareImagesConfig(repoRoot, sharedDir string) error {
	if err := os.MkdirAll(sharedDir, 0o700); err != nil {
		return err
	}
	return prepareYAMLTemplate(repoRoot, "images.example.yaml", filepath.Join(sharedDir, "images.yaml"), nil)
}
