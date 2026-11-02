package config

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestImageBuildConfigRepository(t *testing.T) {
	for _, tc := range []struct{ name, baseImage, want string }{
		{"registry and repository", "registry.example.com/team/base", "registry.example.com/team/base"},
		{"tag", "registry.example.com/team/base:stable", "registry.example.com/team/base"},
		{"digest", "registry.example.com/team/base@sha256:" + strings.Repeat("a", 64), "registry.example.com/team/base"},
		{"tag and digest", "registry.example.com/team/base:stable@sha256:" + strings.Repeat("a", 64), "registry.example.com/team/base"},
		{"registry port", "registry:5000/team/base:stable", "registry:5000/team/base"},
		{"registry port without namespace", "registry:5000/base", "registry:5000/base"},
		{"implicit default registry", "team/base:stable", "team/base"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (ImageBuildConfig{BaseImage: tc.baseImage}).Repository(); got != tc.want {
				t.Fatalf("Repository() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadEnvironmentPrebuildBaseImageRegistry(t *testing.T) {
	prepareLoadTest(t)
	for _, tc := range []struct {
		name      string
		baseImage string
		wantErr   bool
	}{
		{"registry host", "registry.example.com/team/base", false},
		{"registry host with tag", "registry.example.com/team/base:stable", false},
		{"registry host with digest", "registry.example.com/team/base@sha256:" + strings.Repeat("a", 64), false},
		{"registry host with port", "registry:5000/team/base:stable", false},
		{"localhost with port", "localhost:5000/team/base", false},
		{"localhost without port", "localhost/team/base", false},
		{"explicit default registry", "docker.io/team/base", false},
		{"namespace without registry", "team/base:stable", true},
		{"library namespace without registry", "library/ubuntu", true},
		{"repository without slash", "team", true},
		{"empty repository", "registry.example.com/", true},
		{"empty base image", "", true},
		{"url scheme", "https://registry.example.com/team/base", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfigTestYAML(t, environmentPrebuildRegistryYAML(tc.baseImage))
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), "environment_prebuilds.image.base_image ") {
				t.Fatalf("error = %v, want environment_prebuilds.image.base_image validation error", err)
			}
		})
	}
}

func environmentPrebuildRegistryYAML(baseImage string) string {
	return fmt.Sprintf(`
e2b:
  api_url: https://cubesandbox.example
  api_key: key
environment_prebuilds:
  enabled: true
  image:
    base_image: %q
    flow:
      pipeline_url: https://aliyun-flow.example/pipelines/1
      token: token
`, baseImage)
}

func TestLoadEnvironmentPrebuildTemplatePorts(t *testing.T) {
	prepareLoadTest(t)
	for _, tc := range []struct {
		name      string
		template  string
		wantErr   string
		wantPorts []int
		wantProbe int
	}{
		{name: "zero port", template: "exposed_ports: [0, 49983]", wantErr: "environment_prebuilds.template.exposed_ports "},
		{name: "port above range", template: "exposed_ports: [65536, 49983]", wantErr: "environment_prebuilds.template.exposed_ports "},
		{name: "empty ports", template: "exposed_ports: []", wantErr: "environment_prebuilds.template.probe_port "},
		{name: "probe port not exposed", template: "probe_port: 8080", wantErr: "environment_prebuilds.template.probe_port "},
		{name: "defaults", template: "disk_size: 20G", wantPorts: []int{3001, 49983}, wantProbe: 49983},
		{name: "configured ports", template: "exposed_ports: [9000, 3002, 9000]\n    probe_port: 9000", wantPorts: []int{3002, 9000}, wantProbe: 9000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadConfigTestYAML(t, environmentPrebuildRegistryYAML("registry.example.com/team/base")+"  template:\n    "+tc.template+"\n")
			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want prefix %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			template := cfg.EnvironmentPrebuilds.Template
			if !slices.Equal(template.ExposedPorts, tc.wantPorts) || template.ProbePort != tc.wantProbe {
				t.Fatalf("ports = %v probe %d, want %v probe %d", template.ExposedPorts, template.ProbePort, tc.wantPorts, tc.wantProbe)
			}
		})
	}
}
