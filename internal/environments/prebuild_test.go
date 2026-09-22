package environments

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestPrebuildCubeSandboxContracts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template config.TemplateBuildConfig
		request  string
	}{
		{
			name:     "cluster resources and network defaults",
			template: config.TemplateBuildConfig{DiskSize: "10G", Network: config.TemplateNetworkConfig{AllowInternet: true, InjectEgressCA: true}},
			request:  `{"image":"repo:attempt","writableLayerSize":"10G","exposedPorts":[49983],"probePort":49983,"probePath":"/health","allowInternetAccess":true,"with_cube_ca":true}`,
		},
		{
			name: "custom resources and restricted network",
			template: config.TemplateBuildConfig{
				DiskSize: "20G", CPU: 2000, Memory: 4096,
				Network:      config.TemplateNetworkConfig{DNSServers: []string{"10.0.0.2"}, AllowOutboundCIDRs: []string{"10.0.0.0/8"}, DenyOutboundCIDRs: []string{"169.254.0.0/16"}},
				RegistryAuth: config.RegistryAuthConfig{Username: "test-user", Password: "test-password"},
			},
			request: `{"image":"repo:attempt","registryUsername":"test-user","registryPassword":"test-password","writableLayerSize":"20G","cpu":2000,"memory":4096,"dns":["10.0.0.2"],"allowOut":["10.0.0.0/8"],"denyOut":["169.254.0.0/16"],"allowInternetAccess":false,"with_cube_ca":false,"exposedPorts":[49983],"probePort":49983,"probePath":"/health"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests int
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer key" {
					t.Error("missing CubeSandbox auth")
				}
				switch r.Method + " " + r.URL.Path {
				case "POST /templates":
					requests++
					var got, want map[string]any
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Error(err)
					}
					if err := json.Unmarshal([]byte(tc.request), &want); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("template request = %+v, want %+v", got, want)
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, `{"jobID":"j1","templateID":"t1"}`)
				case "GET /templates/t1/builds/j1/status":
					_, _ = io.WriteString(w, `{"status":"ready","message":"done"}`)
				default:
					t.Error(r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			cfg := config.Config{EnvironmentPrebuilds: config.EnvironmentPrebuildConfig{Template: tc.template}, E2B: config.E2BConfig{APIURL: server.URL, APIKey: "key"}}
			c := NewPrebuilds(nil, cfg).templates.(*cubeSandboxTemplateBuilder)
			c.http.client = server.Client()
			if err := c.Cancel(context.Background(), "anything"); !errors.Is(err, errPrebuildUnsupported) {
				t.Fatal(err)
			}
			if _, err := c.ReadLogs(context.Background(), "anything", ""); !errors.Is(err, errPrebuildUnsupported) {
				t.Fatal(err)
			}
			ref, err := c.Start(context.Background(), "repo:attempt")
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("template requests = %d, want 1", requests)
			}
			status, err := c.GetStatus(context.Background(), ref)
			if err != nil || status.State != "succeeded" || status.TemplateID != "t1" {
				t.Fatal(status, err)
			}
			if strings.Contains(string(ref), "test-user") || strings.Contains(string(ref), "test-password") {
				t.Fatal("registry credentials leaked into job reference")
			}
		})
	}
}

func TestPrebuildProviderConfiguration(t *testing.T) {
	base := config.Config{
		EnvironmentPrebuilds: config.EnvironmentPrebuildConfig{Enabled: true, Timeout: time.Hour,
			Image:    config.ImageBuildConfig{BaseImage: "registry/base@sha256:pinned", Flow: config.AliyunFlowConfig{PipelineURL: "https://flow.example/1", Token: "token"}},
			Template: config.TemplateBuildConfig{DiskSize: "10G", Network: config.TemplateNetworkConfig{AllowInternet: true, InjectEgressCA: true}},
		},
		E2B: config.E2BConfig{APIURL: "https://cube.example", APIKey: "key", Domain: "sandbox.example"},
	}
	key := prebuildProviderKey(base)
	for _, tc := range []struct {
		name   string
		change func(*config.Config)
		same   bool
	}{
		{"pipeline", func(c *config.Config) { c.EnvironmentPrebuilds.Image.Flow.PipelineURL = "https://flow.example/2" }, false},
		{"base repository", func(c *config.Config) { c.EnvironmentPrebuilds.Image.BaseImage = "registry/other@sha256:pinned" }, false},
		{"template endpoint", func(c *config.Config) { c.E2B.APIURL = "https://other.example" }, false},
		{"runtime domain", func(c *config.Config) { c.E2B.Domain = "other.example" }, true},
		{"disk", func(c *config.Config) { c.EnvironmentPrebuilds.Template.DiskSize = "20G" }, false},
		{"cpu", func(c *config.Config) { c.EnvironmentPrebuilds.Template.CPU = 2000 }, false},
		{"memory", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Memory = 4096 }, false},
		{"dns", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.DNSServers = []string{"10.0.0.2"} }, false},
		{"empty dns", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.DNSServers = []string{} }, true},
		{"empty allow CIDRs", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.AllowOutboundCIDRs = []string{} }, true},
		{"empty deny CIDRs", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.DenyOutboundCIDRs = []string{} }, true},
		{"allow CIDRs", func(c *config.Config) {
			c.EnvironmentPrebuilds.Template.Network.AllowOutboundCIDRs = []string{"10.0.0.0/8"}
		}, false},
		{"deny CIDRs", func(c *config.Config) {
			c.EnvironmentPrebuilds.Template.Network.DenyOutboundCIDRs = []string{"169.254.0.0/16"}
		}, false},
		{"internet", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.AllowInternet = false }, false},
		{"CA injection", func(c *config.Config) { c.EnvironmentPrebuilds.Template.Network.InjectEgressCA = false }, false},
		{"registry credentials", func(c *config.Config) {
			c.EnvironmentPrebuilds.Template.RegistryAuth = config.RegistryAuthConfig{Username: "rotated", Password: "rotated"}
		}, true},
		{"flow token", func(c *config.Config) { c.EnvironmentPrebuilds.Image.Flow.Token = "rotated" }, true},
		{"sandbox token", func(c *config.Config) { c.E2B.APIKey = "rotated" }, true},
		{"snapshotted base image", func(c *config.Config) { c.EnvironmentPrebuilds.Image.BaseImage = "registry/base@sha256:new" }, true},
		{"timeout", func(c *config.Config) { c.EnvironmentPrebuilds.Timeout = 2 * time.Hour }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.change(&cfg)
			if same := prebuildProviderKey(cfg) == key; same != tc.same {
				t.Fatalf("same provider configuration = %v, want %v", same, tc.same)
			}
		})
	}
}

func TestPrebuildRecipePreservesLiteralSpecs(t *testing.T) {
	p := &environmentPackages{APT: []string{"vim=1.2", "$(touch /tmp/injected)\nCOPY secret /"}, Cargo: []string{"ripgrep"}, Gem: []string{"rake"}, Go: []string{"a@v1", "b@v2"}, NPM: []string{"$(touch /tmp/injected)\nCOPY secret /"}, PIP: []string{"demo >= 1"}}
	file := packageDockerfile("registry/base@sha256:pinned", p)
	var actual [][]string
	for _, line := range strings.Split(file, "\n") {
		if strings.HasPrefix(line, "RUN ") {
			var argv []string
			if err := json.Unmarshal([]byte(line[4:]), &argv); err != nil {
				t.Fatal(err)
			}
			actual = append(actual, argv)
		}
	}
	expected := [][]string{{"sh", "-c", `apt-get update && exec apt-get install -y -- "$@"`, "apt-get", p.APT[0], p.APT[1]}, {"cargo", "install", "ripgrep"}, {"gem", "install", "rake"}, {"go", "install", "a@v1"}, {"go", "install", "b@v2"}, {"npm", "install", "--global", "--", p.NPM[0]}, {"pip", "install", "demo >= 1"}}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("argv=%v", actual)
	}
	if strings.Contains(file, "\nCOPY secret") {
		t.Fatal("package escaped Dockerfile instruction")
	}
}

func TestPrebuildNormalizedPackagesEquality(t *testing.T) {
	base, err := decodeEnvironmentPackages([]byte(`{"type":"cloud","packages":{"apt":["a","b"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, config string
		same         bool
	}{
		{"version changes identity", `{"type":"cloud","packages":{"apt":["a=2","b"]}}`, false},
		{"manager changes identity", `{"type":"cloud","packages":{"pip":["a","b"]}}`, false},
		{"order changes identity", `{"type":"cloud","packages":{"apt":["b","a"]}}`, false},
		{"empty lists and unrelated settings are ignored", `{"type":"cloud","packages":{"apt":["a","b"],"pip":[]},"environment_variables":{"LANG":"en"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			packages, err := decodeEnvironmentPackages([]byte(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			if samePackages(base, packages) != tc.same {
				t.Fatalf("packages=%+v, want same=%v", packages, tc.same)
			}
		})
	}
}

func TestPrebuildTemplateBinding(t *testing.T) {
	jobID := int64(42)
	provider := e2bruntime.NewProvider(config.E2BConfig{Template: "new-base"})
	for _, tc := range []struct {
		name     string
		jobID    *int64
		template string
		resolved string
		ready    bool
	}{
		{"unbuilt environment retains its base", nil, "old-base", "old-base", false},
		{"pending uses provider fallback", &jobID, "", "new-base", false},
		{"ready template survives base change", &jobID, "built-template", "built-template", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := db.Environment{BuildJobID: tc.jobID, ResolvedTemplate: tc.template}
			ready := hasPrebuiltTemplate(env)
			resolution, err := provider.Resolve(env, nil)
			if err != nil || ready != tc.ready || resolution.Template != tc.resolved {
				t.Fatalf("template=%q ready=%v error=%v", resolution.Template, ready, err)
			}
		})
	}
}

func TestPrebuildFailedResponse(t *testing.T) {
	jobID := int64(42)
	service := &Prebuilds{cfg: config.EnvironmentPrebuildConfig{Enabled: true}, providerKey: "provider", images: &aliyunFlowImageBuilder{}, templates: &cubeSandboxTemplateBuilder{}}
	env := db.Environment{BuildJobID: &jobID}
	now := time.Now().UTC()
	build := prebuildTask{job: &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: jobID, CreatedAt: now, FinalizedAt: &now, State: rivertype.JobStateDiscarded},
		Args:   prebuildJobArgs{ProviderKey: "provider"},
	}, output: prebuildJobOutput{ImageJobRef: "image-job"}}
	current := service.currentPrebuildResponse(env, build)
	if current.State != "failed" || current.JobID != "42" || !current.HasImageLogs || !current.CanStart || current.CanCancel || current.CreatedAt == nil || current.FinishedAt == nil {
		t.Fatalf("incorrect failed job capabilities: %+v", current)
	}
	service.providerKey = "changed"
	current = service.currentPrebuildResponse(env, build)
	if current.HasImageLogs || !current.CanStart {
		t.Fatal("new provider must allow a fresh job without accessing old logs")
	}
}
