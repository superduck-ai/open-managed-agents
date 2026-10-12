package config

import (
	"reflect"
	"testing"
)

func TestDeploymentPackageSourcesRejectInvalidManagersAndURLs(t *testing.T) {
	prepareLoadTest(t)
	for _, input := range []string{
		`apt: {sources: []}`, `unknown: {}`, `network: {private_endpoints: []}`,
		`pip: {index_url: "http://packages.example.internal/simple/"}`,
		`go: {proxy: "https://user:secret@packages.example.internal/"}`,
		`cargo: {registries: {"bad.name": "https://packages.example.internal/index/"}}`,
		`cargo: {registries: {example: ""}}`,
		`gem: {sources: ["https://packages.example.internal/?token=secret"]}`,
		`npm: {registry: " https://packages.example.internal/npm/"}`,
		`pip: {extra_index_url: "https://packages.example.internal/simple/"}`,
		`go: {sumdb: "https://packages.example.internal/sumdb/"}`,
		`cargo: {registries: {"bad key": "https://packages.example.internal/index/"}}`,
		`gem: {sources: [""]}`,
		`pip: {index_url: "https://packages.example.internal:/simple/"}`,
		`pip: {index_url: "https://packages.example.internal:8443/simple/"}`,
		`go: {proxy: "https://packages.example.internal/%24SECRET"}`,
		`cargo: {registries: {example: "https://packages.example.internal/%5cpath"}}`,
		`gem: {sources: ["https://packages.example.internal/%09path"]}`,
		`gem: {sources: ["https://packages.example.internal/#"]}`,
	} {
		if _, err := loadConfigTestYAML(t, "environment_packages:\n  "+input+"\n"); err == nil {
			t.Fatalf("accepted invalid package sources: %s", input)
		}
	}
}

func TestDeploymentPackageSourcesOmittedManagersRetainDefaults(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `environment_packages:
  npm:
    scoped_registries:
      "@example": "https://packages.example.internal:443/npm/"
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnvironmentPackages.NPM.Registry != "" || cfg.EnvironmentPackages.PIP.IndexURL != "" || cfg.EnvironmentPackages.Go.Proxy != "" || len(cfg.EnvironmentPackages.Cargo.Registries) != 0 || len(cfg.EnvironmentPackages.Gem.Sources) != 0 {
		t.Fatal("scoped source changed an unspecified default")
	}
}

func TestDeploymentPackageSourcesShareRegistryHostCatalog(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `environment_packages:
  npm:
    scoped_registries:
      "@example": "https://packages.example.internal/npm/"
  pip:
    index_url: "https://pypi.example.internal/simple/"
  go:
    proxy: "https://go.example.internal/proxy/"
  cargo:
    registries:
      example: "https://cargo.example.internal/index/"
  gem:
    sources: ["https://packages.example.internal/gems/"]
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"cargo.example.internal", "go.example.internal", "packages.example.internal", "pypi.example.internal"}
	if got := cfg.EnvironmentPackages.RegistryHosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registry hosts = %v, want %v", got, want)
	}
}
