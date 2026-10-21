package devinit

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

func PrepareLocal(repoRoot string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return prepareLocal(repoRoot, filepath.Join(home, ".oma-dev"))
}

func prepareLocal(repoRoot, sharedDir string) error {
	runDir := filepath.Join(repoRoot, ".run")
	unlock, err := acquireInitLock(runDir)
	if err != nil {
		return err
	}
	defer unlock()
	configExists, err := privateInputExists(baseConfigPath(repoRoot))
	if err != nil {
		return err
	}
	initPath := filepath.Join(runDir, "dev-init.yaml")
	initExists, err := privateInputExists(initPath)
	if err != nil {
		return err
	}
	if configExists && initExists {
		return nil
	}
	if _, bound, err := LoadBinding(envFilePath(repoRoot)); err != nil {
		return err
	} else if bound {
		return fmt.Errorf("bound environment input is missing; restore .run/config.yaml and .run/dev-init.yaml instead of generating new secrets")
	}
	if _, found, err := LoadState(filepath.Join(generatedDir(repoRoot), "state.json")); err != nil {
		return err
	} else if found {
		return fmt.Errorf("environment state exists without complete inputs; restore the original .run configuration and binding")
	}
	for _, dir := range []string{runDir, filepath.Join(runDir, "secrets"), sharedDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	if !configExists {
		if err := prepareLocalSecrets(runDir); err != nil {
			return err
		}
	}
	sharedProviders := filepath.Join(sharedDir, "model-providers.yaml")
	if err := prepareYAMLTemplate(repoRoot, "model-providers.example.yaml", sharedProviders, nil); err != nil {
		return err
	}
	if !configExists {
		if err := prepareYAMLTemplate(repoRoot, "config.example.yaml", baseConfigPath(repoRoot), configureLocalRuntime); err != nil {
			return err
		}
	}
	if !initExists {
		return prepareYAMLTemplate(repoRoot, "dev-init.example.yaml", initPath, func(root *yaml.Node) {
			setMappingValue(root, "model_providers_file", sharedProviders)
		})
	}
	return nil
}

func privateInputExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("private input %s must be a regular file", path)
	}
	return true, os.Chmod(path, 0o600)
}

func prepareYAMLTemplate(repoRoot, name, target string, configure func(*yaml.Node)) error {
	if exists, err := privateInputExists(target); err != nil || exists {
		return err
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "config", name))
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("template %s must contain one YAML mapping", name)
	}
	if configure != nil {
		configure(document.Content[0])
	}
	data, err = yaml.Marshal(&document)
	if err != nil {
		return err
	}
	return createPrivateInput(target, data)
}

func configureLocalRuntime(root *yaml.Node) {
	setMappingValue(root, "environment_runner", "agent_mode", "brain")
	setMappingValue(root, "code_session", "brain_claim_token_file", "./generated/secrets/brain-claim-token")
	setMappingValue(root, "code_session", "jwt_signing_private_key_file", "./secrets/code-session-jwt-ed25519.pem")
	setMappingValue(root, "code_session", "upstream_proxy_ca_key_file", "./secrets/upstream-proxy-ca-key.pem")
	setMappingValue(root, "vault", "master_key", "local", "kek", "")
	setMappingValue(root, "vault", "master_key", "local", "kek_file", "./secrets/vault-kek")
}

func createPrivateInput(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".prepare-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Link(temp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	_, err = privateInputExists(path)
	return err
}

func prepareLocalSecrets(runDir string) error {
	for name, generate := range map[string]func() ([]byte, error){
		"code-session-jwt-ed25519.pem": generateSigningKey,
		"upstream-proxy-ca-key.pem":    generateProxyKey,
		"vault-kek":                    generateLocalKEK,
	} {
		path := filepath.Join(runDir, "secrets", name)
		if exists, err := privateInputExists(path); err != nil {
			return err
		} else if exists {
			continue
		}
		data, err := generate()
		if err != nil {
			return err
		}
		if err := createPrivateInput(path, data); err != nil {
			return err
		}
	}
	return nil
}

func generateSigningKey() ([]byte, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return marshalPrivateKey(private)
}

func generateProxyKey() ([]byte, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return marshalPrivateKey(private)
}

func marshalPrivateKey(key any) ([]byte, error) {
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), nil
}

func generateLocalKEK() ([]byte, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(data) + "\n"), nil
}
