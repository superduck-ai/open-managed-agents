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
		{"empty scoped registry", `npm: {scoped_registries: {"@cvte": ""}}`},
		{"duplicate normalized scopes", `npm: {scoped_registries: {" @cvte ": "https://a.example.com/", "@cvte": "https://b.example.com/"}}`},
		{"all private destinations", `network: {private_endpoints: [{host: registry.example.com, port: 443, allowed_cidrs: ["0.0.0.0/0"]}]}`},
		{"metadata", `network: {private_endpoints: [{host: registry.example.com, port: 443, allowed_cidrs: ["169.254.169.254/32"]}]}`},
		{"missing CIDR", `network: {private_endpoints: [{host: registry.example.com, port: 443}]}`},
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

func TestLoadEnvironmentPackagesNormalizesRegistryConfiguration(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `
environment_packages:
  npm:
    registry: " https://registry.npmjs.org/ "
    scoped_registries:
      " @cvte ": " https://artifactory.gz.cvte.cn/artifactory/api/npm/npm-local/ "
      "@another": "https://artifactory.gz.cvte.cn/other/"
  network:
    private_endpoints:
      - host: " ARTIFACTORY.GZ.CVTE.CN. "
        port: 443
        allowed_cidrs: [" 10.20.30.40/32 "]
`)
	if err != nil {
		t.Fatal(err)
	}
	options := cfg.EnvironmentPackages.NPM.Options()
	want := []string{"--registry=https://registry.npmjs.org/", "--@another:registry=https://artifactory.gz.cvte.cn/other/", "--@cvte:registry=https://artifactory.gz.cvte.cn/artifactory/api/npm/npm-local/"}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("npm options = %v, want %v", options, want)
	}
	if hosts := cfg.EnvironmentPackages.RegistryHosts(); !reflect.DeepEqual(hosts, []string{"artifactory.gz.cvte.cn", "registry.npmjs.org"}) {
		t.Fatalf("registry hosts = %v", hosts)
	}
	endpoint := cfg.EnvironmentPackages.Network.PrivateEndpoints[0]
	if endpoint.Host != "artifactory.gz.cvte.cn" || endpoint.AllowedCIDRs[0] != "10.20.30.40/32" {
		t.Fatalf("endpoint was not normalized: %+v", endpoint)
	}
	defaults, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults.EnvironmentPackages.NPM.Options()) != 0 || len(defaults.EnvironmentPackages.Network.PrivateEndpoints) != 0 {
		t.Fatal("default configuration must not grant private access or change registries")
	}
}
