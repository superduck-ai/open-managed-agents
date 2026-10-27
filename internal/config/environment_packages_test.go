package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadEnvironmentPackagesRejectsInvalidConfiguration(t *testing.T) {
	prepareLoadTest(t)
	for _, test := range []struct{ name, yaml string }{
		{"HTTP registry", `npm: {registry: "http://registry.example.com/"}`},
		{"URL credentials", `npm: {registry: "https://user:secret@registry.example.com/"}`},
		{"query", `npm: {registry: "https://registry.example.com/?token=secret"}`},
		{"fragment", `npm: {registry: "https://registry.example.com/#secret"}`},
		{"wrong port", `npm: {registry: "https://registry.example.com:8443/"}`},
		{"interpolation", `npm: {registry: "https://registry.example.com/${SECRET}"}`},
		{"newline", `npm: {registry: "https://registry.example.com/\nregistry=secret"}`},
		{"invalid scope", `npm: {scoped_registries: {"--secret": "https://registry.example.com/"}}`},
		{"empty scoped registry", `npm: {scoped_registries: {"@example": ""}}`},
		{"duplicate normalized scopes", `npm: {scoped_registries: {" @example ": "https://a.example.com/", "@example": "https://b.example.com/"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadConfigTestYAML(t, "environment_packages:\n  "+test.yaml+"\n")
			if err == nil || !strings.Contains(err.Error(), "environment_packages") {
				t.Fatalf("error = %v, want configuration rejection", err)
			}
			if strings.Contains(err.Error(), "secret@") || strings.Contains(err.Error(), "token=secret") {
				t.Fatalf("error disclosed URL credentials: %v", err)
			}
		})
	}
}

func TestLoadEnvironmentPackagesUsesDeclaredRegistries(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `
environment_packages:
  npm:
    registry: "https://registry.npmjs.org/"
    scoped_registries:
      "@example": "https://registry.example.internal/npm/"
      "@another": "https://registry.example.internal/other/"
`)
	if err != nil {
		t.Fatal(err)
	}
	options := cfg.EnvironmentPackages.NPM.Options()
	want := []string{"--registry=https://registry.npmjs.org/", "--@another:registry=https://registry.example.internal/other/", "--@example:registry=https://registry.example.internal/npm/"}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("npm options = %v, want %v", options, want)
	}
	if hosts := cfg.EnvironmentPackages.RegistryHosts(); !reflect.DeepEqual(hosts, []string{"registry.example.internal", "registry.npmjs.org"}) {
		t.Fatalf("registry hosts = %v", hosts)
	}
	defaults, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if !defaults.EnvironmentPackages.Empty() {
		t.Fatal("default configuration must not grant private access or change registries")
	}
}
