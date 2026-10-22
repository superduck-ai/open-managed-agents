package devinit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func baseConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".run", "config.yaml")
}

func generatedDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".run", "generated")
}

func loadBaseConfig(repoRoot string) (config.Config, error) {
	images, err := loadImagesConfig()
	if err != nil {
		return config.Config{}, err
	}
	return loadBaseConfigWithImages(repoRoot, images)
}

func loadBaseConfigWithImages(repoRoot string, images imagesConfig) (config.Config, error) {
	configPath := baseConfigPath(repoRoot)
	if _, err := os.Stat(configPath); err != nil {
		return config.Config{}, fmt.Errorf("base config %s is required: %w", configPath, err)
	}
	previous, hadPrevious := os.LookupEnv("CONFIG_FILE")
	if err := os.Setenv("CONFIG_FILE", configPath); err != nil {
		return config.Config{}, err
	}
	defer func() {
		if hadPrevious {
			_ = os.Setenv("CONFIG_FILE", previous)
		} else {
			_ = os.Unsetenv("CONFIG_FILE")
		}
	}()
	base, err := config.Load()
	if err != nil {
		return config.Config{}, err
	}
	base.E2B.Template = images.sandboxTemplate(base.E2B.Template)
	return base, nil
}

func filepathEvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func localSandboxTemplate(template string) string {
	return (imagesConfig{}).sandboxTemplate(template)
}
