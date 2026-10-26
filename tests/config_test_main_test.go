//go:build integration || e2e || e2b_integration

package tests

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMain(m *testing.M) {
	cleanup, err := configureTestYAML()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure test YAML: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func configureTestYAML() (func(), error) {
	if _, configured := os.LookupEnv("CONFIG_FILE"); configured {
		return func() {}, nil
	}
	directory, err := os.MkdirTemp("", "oma-test-config-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	if err := writeDefaultTestYAML(directory); err != nil {
		cleanup()
		return nil, err
	}
	if err := os.Setenv("CONFIG_FILE", filepath.Join(directory, "config.yaml")); err != nil {
		cleanup()
		return nil, err
	}
	return cleanup, nil
}

func writeDefaultTestYAML(directory string) error {
	data, err := os.ReadFile(filepath.Join("..", "config", "config.example.yaml"))
	if err != nil {
		return err
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	keyPath, err := writeTestProxyPrivateKey(directory)
	if err != nil {
		return err
	}
	document["code_session"] = map[string]any{"upstream_proxy_mitm_enabled": true, "upstream_proxy_ca_key_file": keyPath}
	data, err = yaml.Marshal(document)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "config.yaml"), data, 0o600)
}

func writeTestProxyPrivateKey(directory string) (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, "proxy-ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
