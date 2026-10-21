package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSpec = `
version: 1
fixed_secrets_dir: /tmp/fixed-secrets
organization:
  name: OMA Local
user:
  email: test@oma.ai
  name: Local Test
  role: admin
workspaces:
  - ref: default
    name: default
    is_default: true
    api_keys:
      - ref: key-1
        name: default-key-1
      - ref: key-2
        name: default-key-2
  - ref: secondary
    name: secondary
    is_default: false
    api_keys:
      - ref: key-1
        name: secondary-key-1
      - ref: key-2
        name: secondary-key-2
model_providers_file: ./model-providers.yaml
`

func writeSpecFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dev-init.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSpecRejectsMalformedInputs(t *testing.T) {
	cases := map[string]string{
		"unknown field":        strings.Replace(validSpec, "organization:", "mystery: 1\norganization:", 1),
		"multiple documents":   validSpec + "\n---\nversion: 1\n",
		"null value":           strings.Replace(validSpec, "name: OMA Local", "name:", 1),
		"wrong version":        strings.Replace(validSpec, "version: 1", "version: 2", 1),
		"invalid email":        strings.Replace(validSpec, "test@oma.ai", "not-an-email", 1),
		"non admin role":       strings.Replace(validSpec, "role: admin", "role: developer", 1),
		"one workspace":        strings.Replace(validSpec, "  - ref: secondary\n    name: secondary\n    is_default: false\n    api_keys:\n      - ref: key-1\n        name: secondary-key-1\n      - ref: key-2\n        name: secondary-key-2\n", "", 1),
		"duplicate ws ref":     strings.Replace(validSpec, "ref: secondary", "ref: default", 1),
		"no default workspace": strings.Replace(validSpec, "is_default: true", "is_default: false", 1),
		"three key refs":       strings.Replace(validSpec, "      - ref: key-2\n        name: default-key-2\n", "      - ref: key-2\n        name: default-key-2\n      - ref: key-3\n        name: default-key-3\n", 1),
		"duplicate key ref":    strings.Replace(validSpec, "      - ref: key-2\n        name: default-key-2\n", "      - ref: key-1\n        name: default-key-2\n", 1),
		"missing providers":    strings.Replace(validSpec, "model_providers_file: ./model-providers.yaml", "", 1),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadSpec(writeSpecFile(t, content)); err == nil {
				t.Fatalf("LoadSpec() error = nil for %s, want failure", name)
			}
		})
	}
}

func TestLoadSpecResolvesRelativeProviderFile(t *testing.T) {
	path := writeSpecFile(t, strings.Replace(validSpec, "/tmp/fixed-secrets", "./secrets", 1))
	spec, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Version != 1 {
		t.Fatalf("Version = %d, want 1", spec.Version)
	}
	if !filepath.IsAbs(spec.ModelProviders) {
		t.Fatalf("ModelProviders = %q, want absolute path", spec.ModelProviders)
	}
	if spec.FixedSecretsDir != filepath.Join(filepath.Dir(path), "secrets") {
		t.Fatalf("FixedSecretsDir = %q, want secrets beside the init file", spec.FixedSecretsDir)
	}
	if len(spec.Workspaces) != 2 || len(spec.Workspaces[0].APIKeys) != 2 {
		t.Fatalf("unexpected workspaces: %+v", spec.Workspaces)
	}
}

func writeProvidersFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model-providers.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validProviders = `
version: 1
providers:
  - name: primary
    api_backend: messages
    base_url: https://api.anthropic.com
    api_key: sk-test-key
    model_ids:
      - model-a
  - name: compatible
    api_backend: chat_completions
    base_url: https://provider.example
    api_key_file: ./compatible-api-key
    model_ids:
      - model-b
`

func TestLoadModelProvidersRejectsMalformedInputs(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "compatible-api-key")
	if err := os.WriteFile(keyFile, []byte("file-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := strings.Replace(validProviders, "./compatible-api-key", keyFile, 1)
	cases := map[string]string{
		"multiple documents":  base + "\n---\nversion: 1\n",
		"unknown field":       strings.Replace(base, "providers:", "mystery: 1\nproviders:", 1),
		"invalid slug":        strings.Replace(base, "name: primary", "name: primary\n    slug: Invalid/slug", 1),
		"duplicate slug":      strings.Replace(strings.Replace(base, "name: primary", "name: primary\n    slug: shared", 1), "name: compatible", "name: compatible\n    slug: shared", 1),
		"bad backend":         strings.Replace(base, "chat_completions", "grpc", 1),
		"url with userinfo":   strings.Replace(base, "https://provider.example", "https://user:pass@provider.example", 1),
		"two key sources":     strings.Replace(base, "api_key_file: ", "api_key: inline\n    api_key_file: ", 1),
		"no key source":       strings.Replace(strings.Replace(base, "api_key: sk-test-key", "", 1), "api_key_file: "+keyFile, "", 1),
		"missing env":         strings.Replace(base, "api_key: sk-test-key", "api_key_env: OMA_TEST_MISSING_ENV_KEY", 1),
		"empty model list":    strings.Replace(base, "      - model-b\n", "", 1),
		"duplicate model":     strings.Replace(base, "      - model-b", "      - model-a\n      - model-a", 1),
		"duplicate name":      strings.Replace(base, "name: compatible", "name: primary", 1),
		"empty provider list": strings.Replace(base, "  - name: primary\n    api_backend: messages\n    base_url: https://api.anthropic.com\n    api_key: sk-test-key\n    model_ids:\n      - model-a\n  - name: compatible\n    api_backend: chat_completions\n    base_url: https://provider.example\n    api_key_file: "+keyFile+"\n    model_ids:\n      - model-b\n", "", 1),
	}
	t.Setenv("OMA_TEST_MISSING_ENV_KEY", "")
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadModelProviders(writeProvidersFile(t, content)); err == nil {
				t.Fatalf("LoadModelProviders() error = nil for %s, want failure", name)
			}
		})
	}
}

func TestLoadModelProvidersResolvesKeys(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "compatible-api-key")
	if err := os.WriteFile(keyFile, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OMA_TEST_ENV_KEY", "env-key")
	content := strings.Replace(validProviders, "./compatible-api-key", keyFile, 1)
	content = strings.Replace(content, "api_key: sk-test-key", "api_key_env: OMA_TEST_ENV_KEY", 1)
	content = strings.Replace(content, "name: primary", "name: primary\n    slug: primary", 1)
	spec, err := LoadModelProviders(writeProvidersFile(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(spec.Providers))
	}
	if spec.Providers[0].Slug != "primary" || spec.Providers[1].Slug != "" {
		t.Fatalf("unexpected provider slugs: %q, %q", spec.Providers[0].Slug, spec.Providers[1].Slug)
	}
	if spec.Providers[0].APIKey != "env-key" {
		t.Fatalf("Providers[0].APIKey = %q, want env-key", spec.Providers[0].APIKey)
	}
	if spec.Providers[1].APIKey != "file-key" {
		t.Fatalf("Providers[1].APIKey = %q, want trimmed file-key", spec.Providers[1].APIKey)
	}
}
