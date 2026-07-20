package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if err := configureTestYAML(); err != nil {
		fmt.Fprintf(os.Stderr, "configure test YAML: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func configureTestYAML() error {
	if _, configured := os.LookupEnv("CONFIG_FILE"); configured {
		return nil
	}

	examplePath, err := filepath.Abs(filepath.Join("..", "config", "config.example.yaml"))
	if err != nil {
		return err
	}
	return os.Setenv("CONFIG_FILE", examplePath)
}
