package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
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
			template: config.TemplateBuildConfig{DiskSize: "10G", Network: config.TemplateNetworkConfig{AllowInternet: true, InjectEgressCA: true}, ExposedPorts: []int{3001, 49983}, ProbePort: 49983},
			request:  `{"image":"repo:attempt","writableLayerSize":"10G","exposedPorts":[3001,49983],"probePort":49983,"probePath":"/health","allowInternetAccess":true,"with_cube_ca":true}`,
		},
		{
			name: "custom resources and restricted network",
			template: config.TemplateBuildConfig{
				DiskSize: "20G", CPU: 2000, Memory: 4096, ExposedPorts: []int{3001, 49983}, ProbePort: 49983,
				Network:      config.TemplateNetworkConfig{DNSServers: []string{"10.0.0.2"}, AllowOutboundCIDRs: []string{"10.0.0.0/8"}, DenyOutboundCIDRs: []string{"169.254.0.0/16"}},
				RegistryAuth: config.RegistryAuthConfig{Username: "test-user", Password: "test-password"},
			},
			request: `{"image":"repo:attempt","registryUsername":"test-user","registryPassword":"test-password","writableLayerSize":"20G","cpu":2000,"memory":4096,"dns":["10.0.0.2"],"allowOut":["10.0.0.0/8"],"denyOut":["169.254.0.0/16"],"allowInternetAccess":false,"with_cube_ca":false,"exposedPorts":[3001,49983],"probePort":49983,"probePath":"/health"}`,
		},
		{
			name:     "configured exposed and probe ports",
			template: config.TemplateBuildConfig{DiskSize: "10G", ExposedPorts: []int{8080, 9000}, ProbePort: 9000},
			request:  `{"image":"repo:attempt","writableLayerSize":"10G","exposedPorts":[8080,9000],"probePort":9000,"probePath":"/health","allowInternetAccess":false,"with_cube_ca":false}`,
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
			svc := NewPrebuilds(nil, cfg, nil)
			c := svc.templates.(*cubeSandboxTemplateBuilder)
			c.http.client = server.Client()
			if err := c.Cancel(context.Background(), "anything"); !errors.Is(err, errPrebuildUnsupported) {
				t.Fatal(err)
			}
			if _, err := c.ReadLogs(context.Background(), "anything", ""); !errors.Is(err, errPrebuildUnsupported) {
				t.Fatal(err)
			}
			input := svc.templateInput(templateResources{})
			input.ImageRef = "repo:attempt"
			ref, err := c.Start(context.Background(), input)
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
			Template: config.TemplateBuildConfig{DiskSize: "10G", Network: config.TemplateNetworkConfig{AllowInternet: true, InjectEgressCA: true}, ExposedPorts: []int{3001, 49983}, ProbePort: 49983},
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
		{"exposed ports", func(c *config.Config) { c.EnvironmentPrebuilds.Template.ExposedPorts = []int{8080, 49983} }, false},
		{"probe port", func(c *config.Config) { c.EnvironmentPrebuilds.Template.ProbePort = 8080 }, false},
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
	file := packageDockerfile("registry/base@sha256:pinned", p, config.EnvironmentPackageConfig{})
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
		{"order within a manager is ignored", `{"type":"cloud","packages":{"apt":["b","a"]}}`, true},
		{"repeated entries change identity", `{"type":"cloud","packages":{"apt":["b","a","a"]}}`, false},
		{"added package changes identity", `{"type":"cloud","packages":{"apt":["b","a","c"]}}`, false},
		{"removed package changes identity", `{"type":"cloud","packages":{"apt":["a"]}}`, false},
		{"pinned version changes identity", `{"type":"cloud","packages":{"apt":["b","a=1.0"]}}`, false},
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

func TestPrebuildReconcileUnchangedPackagesKeepsCorrectTemplate(t *testing.T) {
	jobID := int64(42)
	handler := &Handler{cfg: config.Config{E2B: config.E2BConfig{Template: "base-b"}}}
	service := &Prebuilds{}
	provider := e2bruntime.NewProvider(config.E2BConfig{Template: "base-b"})
	for _, tc := range []struct {
		name           string
		config         string
		jobID          *int64
		storedTemplate string
		wantTemplate   string
	}{
		{"environment without prebuild refreshes base", `{"type":"cloud"}`, nil, "base-a", "base-b"},
		{"completed prebuild keeps its template", `{"type":"cloud","packages":{"pip":["requests"]}}`, &jobID, "built-template", "built-template"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := db.Environment{Config: json.RawMessage(tc.config), BuildJobID: tc.jobID, ResolvedBuildJobID: tc.jobID, ResolvedTemplate: tc.storedTemplate}
			next, err := handler.applyEnvironmentMutation(current, environmentMutationRequest{Config: json.RawMessage(`{"type":"cloud"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if err := service.reconcilePrebuild(context.Background(), nil, &current, &next); err != nil {
				t.Fatal(err)
			}
			resolution, err := provider.Resolve(next, nil)
			if err != nil || resolution.Template != tc.wantTemplate || next.BuildJobID != tc.jobID {
				t.Fatalf("reconciled environment = (%q, %v), resolve = (%q, %v); want template %q and original job", next.ResolvedTemplate, next.BuildJobID, resolution.Template, err, tc.wantTemplate)
			}
		})
	}
}

func TestPrebuildReconcileRepairsInvalidStoredPackages(t *testing.T) {
	jobID := int64(42)
	current := db.Environment{
		Config:             json.RawMessage(`{"type":"cloud","packages":{"apt":"vim"}}`),
		BuildJobID:         &jobID,
		ResolvedBuildJobID: &jobID,
		ResolvedTemplate:   "old-template",
	}
	handler := &Handler{cfg: config.Config{E2B: config.E2BConfig{Template: "base-b"}}}
	next, err := handler.applyEnvironmentMutation(current, environmentMutationRequest{
		Config: json.RawMessage(`{"type":"cloud","packages":{}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Prebuilds{}).reconcilePrebuild(context.Background(), nil, &current, &next); err != nil {
		t.Fatal(err)
	}
	if next.BuildJobID != nil || next.ResolvedBuildJobID != nil || next.ResolvedTemplate != "base-b" {
		t.Fatalf("repaired environment kept stale prebuild: job=%v template=%q", next.BuildJobID, next.ResolvedTemplate)
	}
}

func TestPrebuildReconcileRejectsInvalidReplacementPackages(t *testing.T) {
	current := db.Environment{Config: json.RawMessage(`{"type":"cloud"}`)}
	next := db.Environment{Config: json.RawMessage(`{"type":"cloud","packages":{"apt":"vim"}}`)}
	if err := (&Prebuilds{}).reconcilePrebuild(context.Background(), nil, &current, &next); err == nil {
		t.Fatal("invalid replacement packages accepted")
	}
}

func TestPrebuildReconcileTemplateTransition(t *testing.T) {
	active, pending := int64(41), int64(42)
	handler := &Handler{cfg: config.Config{E2B: config.E2BConfig{Template: "base"}, EnvironmentPrebuilds: config.EnvironmentPrebuildConfig{Enabled: true, Template: config.TemplateBuildConfig{MaxResources: config.TemplateResourceLimit{CPU: 8000}}}}}
	before := `{"type":"cloud","packages":{"type":"packages","pip":["a","b"]},"init_script":"make","resources":{"cpu":1000},"environment":{"A":"1"}}`
	packages := json.RawMessage(`{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":["a","b"]}`)
	for _, tc := range []struct {
		name         string
		update       string
		archived     bool
		disabled     bool
		wantEnqueue  bool
		wantTemplate string
		wantActive   *int64
	}{
		{"environment variables only keep the build", `{"environment":{"A":"2"}}`, false, false, false, "tpl-old", &active},
		{"networking only keeps the build", `{"networking":{"type":"limited","allowed_hosts":["example.com"]}}`, false, false, false, "tpl-old", &active},
		{"package order only keeps the build", `{"packages":{"type":"packages","pip":["b","a"]}}`, false, false, false, "tpl-old", &active},
		{"package change rebuilds and keeps the previous template", `{"packages":{"type":"packages","pip":["a","b","c"]}}`, false, false, true, "tpl-old", &active},
		{"package version change rebuilds", `{"packages":{"type":"packages","pip":["a==1.0","b"]}}`, false, false, true, "tpl-old", &active},
		{"init script change rebuilds", `{"init_script":"make all"}`, false, false, true, "tpl-old", &active},
		{"init script removal rebuilds while packages remain", `{"init_script":null}`, false, false, true, "tpl-old", &active},
		{"resources change rebuilds", `{"resources":{"cpu":2000}}`, false, false, true, "tpl-old", &active},
		{"clearing the build spec returns to default", `{"packages":null,"init_script":null,"resources":null}`, false, false, false, "base", nil},
		{"archived environment returns to default", `{"init_script":"make all"}`, true, false, false, "base", nil},
		{"disabled prebuilds return to default", `{"packages":{"type":"packages","pip":["c"]}}`, false, true, false, "base", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := db.Environment{Config: json.RawMessage(before), BuildJobID: &pending, ResolvedBuildJobID: &active, ResolvedTemplate: "tpl-old", ResolvedPackages: packages}
			if tc.archived {
				now := time.Now()
				current.ArchivedAt = &now
			}
			next, err := handler.applyEnvironmentMutation(current, environmentMutationRequest{Config: json.RawMessage(tc.update)})
			if err != nil {
				t.Fatal(err)
			}
			service := &Prebuilds{cfg: config.EnvironmentPrebuildConfig{Enabled: !tc.disabled}}
			err = service.reconcilePrebuild(context.Background(), nil, &current, &next)
			if enqueued := errors.Is(err, errPrebuildUnavailable); enqueued != tc.wantEnqueue || (err != nil && !enqueued) {
				t.Fatalf("reconcile error = %v, want enqueue=%v", err, tc.wantEnqueue)
			}
			if next.ResolvedTemplate != tc.wantTemplate || !reflect.DeepEqual(next.ResolvedBuildJobID, tc.wantActive) || (len(next.ResolvedPackages) > 0) != (tc.wantActive != nil) {
				t.Fatalf("template=%q active=%v packages=%s", next.ResolvedTemplate, next.ResolvedBuildJobID, next.ResolvedPackages)
			}
			if !tc.wantEnqueue && tc.wantActive != nil && !reflect.DeepEqual(next.BuildJobID, current.BuildJobID) {
				t.Fatalf("build job changed to %v", next.BuildJobID)
			}
		})
	}
}

func TestPrebuildReconcileAdoptsTemplateFromOlderInstance(t *testing.T) {
	jobID := int64(42)
	before := `{"type":"cloud","packages":{"type":"packages","pip":["b","a"]},"init_script":"make"}`
	handler := &Handler{cfg: config.Config{E2B: config.E2BConfig{Template: "base"}, EnvironmentPrebuilds: config.EnvironmentPrebuildConfig{Enabled: true}}}
	service := &Prebuilds{cfg: config.EnvironmentPrebuildConfig{Enabled: true}}
	for _, tc := range []struct {
		name        string
		update      string
		wantEnqueue bool
	}{
		{"unchanged spec records the build in place", `{"environment":{"A":"1"}}`, false},
		{"changed spec keeps the adopted template during the rebuild", `{"init_script":"make all"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := db.Environment{Config: json.RawMessage(before), BuildJobID: &jobID, ResolvedTemplate: "tpl-legacy"}
			next, err := handler.applyEnvironmentMutation(current, environmentMutationRequest{Config: json.RawMessage(tc.update)})
			if err != nil {
				t.Fatal(err)
			}
			err = service.reconcilePrebuild(context.Background(), nil, &current, &next)
			if errors.Is(err, errPrebuildUnavailable) != tc.wantEnqueue || (err != nil && !tc.wantEnqueue) {
				t.Fatalf("reconcile error = %v", err)
			}
			if next.ResolvedTemplate != "tpl-legacy" || next.ResolvedBuildJobID == nil || *next.ResolvedBuildJobID != jobID {
				t.Fatalf("template=%q active=%v", next.ResolvedTemplate, next.ResolvedBuildJobID)
			}
			packages, known := decodePackagesSnapshot(next.ResolvedPackages)
			if !known || packages == nil || !reflect.DeepEqual(packages.PIP, []string{"b", "a"}) {
				t.Fatalf("packages snapshot = %s", next.ResolvedPackages)
			}
		})
	}
}

func TestPrebuildResponseReadiness(t *testing.T) {
	active, jobID := int64(41), int64(42)
	failedTask := prebuildTask{job: &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: jobID, State: rivertype.JobStateDiscarded},
		Args:   prebuildJobArgs{ProviderKey: "provider"},
	}}
	resolvedTask := prebuildTask{job: &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: jobID, State: rivertype.JobStateCompleted},
		Args:   prebuildJobArgs{ProviderKey: "provider"},
	}, output: prebuildJobOutput{ImageRef: "image", TemplateID: "tpl-new"}}
	service := &Prebuilds{cfg: config.EnvironmentPrebuildConfig{Enabled: true}, providerKey: "provider", images: &aliyunFlowImageBuilder{}, templates: &cubeSandboxTemplateBuilder{}}
	transition := db.Environment{BuildJobID: &jobID, ResolvedBuildJobID: &active, ResolvedTemplate: "tpl-old"}
	for _, tc := range []struct {
		name      string
		env       db.Environment
		task      prebuildTask
		wantState string
		wantStart bool
	}{
		{"failed rebuild can be retried", transition, failedTask, "failed", true},
		{"completed build is ready", db.Environment{BuildJobID: &jobID, ResolvedBuildJobID: &jobID, ResolvedTemplate: "tpl-new"}, resolvedTask, "ready", false},
		{"template written back by an older instance is ready", db.Environment{BuildJobID: &jobID, ResolvedTemplate: "tpl-new"}, resolvedTask, "ready", false},
		{"job result read after a stale environment is not ready", transition, resolvedTask, "running", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := service.currentPrebuildResponse(tc.env, tc.task)
			if response.State != tc.wantState || response.CanStart != tc.wantStart {
				t.Fatalf("response = %+v", response)
			}
		})
	}
}

func TestPackageSpecNames(t *testing.T) {
	for _, tc := range []struct{ manager, spec, want string }{
		{"apt", "curl=7.88.1-10", "curl"},
		{"apt", "curl/bookworm-backports", "curl"},
		{"apt", "libc6:i386", "libc6:i386"},
		{"pip", "Numpy==2.3.5", "numpy"},
		{"pip", "zope.interface>=6", "zope-interface"},
		{"pip", "requests[socks]~=2.32", "requests"},
		{"pip", "pkg @ git+https://example.com/pkg.git", "pkg"},
		{"npm", "typescript@5.9.3", "typescript"},
		{"npm", "@scope/tool@1.0.0", "@scope/tool"},
		{"npm", "@scope/tool", "@scope/tool"},
		{"gem", "rake:13.2.1", "rake"},
		{"cargo", "ripgrep@14.1.1", "ripgrep"},
		{"go", "golang.org/x/tools/gopls@v0.16.0", "golang.org/x/tools/gopls"},
	} {
		if got := packageSpecName(tc.manager, tc.spec); got != tc.want {
			t.Fatalf("packageSpecName(%q, %q) = %q, want %q", tc.manager, tc.spec, got, tc.want)
		}
	}
}

func TestNormalizePackagesRejectsDuplicateNames(t *testing.T) {
	for _, tc := range []struct {
		name, packages, wantError string
	}{
		{"repeated go module versions", `{"go":["example.com/tool@v1","example.com/tool@v2"]}`, `config.packages.go must not list package "example.com/tool" more than once`},
		{"identical repeated entries", `{"apt":["curl","curl"]}`, `config.packages.apt must not list package "curl" more than once`},
		{"normalized pip names", `{"pip":["Foo_Bar==1","foo-bar"]}`, `config.packages.pip must not list package "foo-bar" more than once`},
		{"scoped npm versions", `{"npm":["@scope/tool@1","@scope/tool@2"]}`, `config.packages.npm must not list package "@scope/tool" more than once`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizePackages(json.RawMessage(tc.packages)); err == nil || err.Error() != tc.wantError {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
		})
	}
	if _, err := normalizePackages(json.RawMessage(`{"npm":["tool@1"],"pip":["tool==1"],"go":["example.com/a@v1","example.com/b@v1"]}`)); err != nil {
		t.Fatalf("distinct packages rejected: %v", err)
	}
	handler := &Handler{cfg: config.Config{EnvironmentPrebuilds: config.EnvironmentPrebuildConfig{Enabled: true}}}
	current := db.Environment{Config: json.RawMessage(`{"type":"cloud"}`)}
	_, err := handler.applyEnvironmentMutation(current, environmentMutationRequest{Config: json.RawMessage(`{"packages":{"type":"packages","go":["example.com/tool@v1","example.com/tool@v2"]}}`)})
	var appError *apperr.Error
	if !errors.As(err, &appError) || appError.Kind != apperr.InvalidArgument || !strings.Contains(appError.PublicMessage, `"example.com/tool"`) {
		t.Fatalf("update error = %v", err)
	}
}

func TestStoredDuplicatePackagesRemainReadable(t *testing.T) {
	config := json.RawMessage(`{"type":"cloud","packages":{"type":"packages","go":["example.com/tool@v1","example.com/tool@v2"]}}`)
	if _, err := decodeEnvironmentBuildSpec(config); err != nil {
		t.Fatalf("decode stored duplicates: %v", err)
	}
	if _, provision, err := buildPackageManifest(config); err != nil || !provision {
		t.Fatalf("manifest = %v, %v", provision, err)
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

func TestPrebuildLogsBeforeStageSubmission(t *testing.T) {
	svc := &Prebuilds{
		cfg:         config.EnvironmentPrebuildConfig{Enabled: true},
		providerKey: "provider",
		images:      &aliyunFlowImageBuilder{},
		templates:   &cubeSandboxTemplateBuilder{},
	}
	task := prebuildTask{job: &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: 42},
		Args:   prebuildJobArgs{ProviderKey: "provider"},
	}}
	for _, stage := range []string{"image", "template"} {
		t.Run(stage, func(t *testing.T) {
			_, err := svc.readLogs(context.Background(), task, stage, "")
			var appError *apperr.Error
			if !errors.As(prebuildError(err), &appError) || appError.Kind != apperr.Conflict || appError.PublicMessage != "Build stage has not started yet" {
				t.Fatalf("readLogs(%q) error = %v, want stage-not-started conflict", stage, err)
			}
		})
	}
}

func TestPrebuildDeletedEnvironmentMapsToNotFound(t *testing.T) {
	var appError *apperr.Error
	if !errors.As(prebuildError(db.ErrNotFound), &appError) || appError.Kind != apperr.NotFound {
		t.Fatalf("prebuildError(db.ErrNotFound) = %v, want not found", appError)
	}
}

func TestSubmittedCheckpointRetainsRemoteRefAcrossWriteFailure(t *testing.T) {
	task := prebuildTask{output: prebuildJobOutput{ImageJobRef: `{"run":"42"}`}}
	persisted := prebuildJobOutput{Submitting: true}
	attempts := 0
	superseded, err := retryCheckpointWrite(context.Background(), time.Millisecond, func() (bool, error) {
		attempts++
		if attempts == 1 {
			return false, errors.New("temporary database failure")
		}
		persisted = task.output
		return false, nil
	})
	if err != nil || superseded || attempts != 2 || persisted.ImageJobRef != `{"run":"42"}` || persisted.Submitting {
		t.Fatalf("checkpoint retry = (superseded=%t, err=%v, attempts=%d, persisted=%+v)", superseded, err, attempts, persisted)
	}
}

func TestSubmittedCheckpointStopsRetryingWhenWorkerStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	_, err := retryCheckpointWrite(ctx, time.Millisecond, func() (bool, error) {
		attempts++
		cancel()
		return false, errors.New("database unavailable")
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("checkpoint retry = (err=%v, attempts=%d), want cancellation after one attempt", err, attempts)
	}
}

func TestBuildHTTPRejectsAndAmbiguousFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		wantStatus int
		rejected   bool
	}{
		{"invalid flow token", http.StatusUnauthorized, http.StatusUnauthorized, true},
		{"invalid template request", http.StatusBadRequest, http.StatusBadRequest, true},
		{"missing pipeline endpoint", http.StatusNotFound, http.StatusNotFound, true},
		{"provider failure after accepting", http.StatusInternalServerError, 0, false},
		{"redirect is not followed", http.StatusFound, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			client := newBuildHTTP("x-yunxiao-token", "token")
			var output json.RawMessage
			err := client.call(context.Background(), http.MethodPost, server.URL+"/runs", nil, &output)
			var providerError *buildProviderError
			if !errors.As(err, &providerError) || providerError.Status != tc.status {
				t.Fatalf("call error = %v, want provider status %d", err, tc.status)
			}
			if want := fmt.Sprintf("build provider returned HTTP %d", tc.status); err.Error() != want {
				t.Fatalf("call error = %q, want %q", err, want)
			}
			status, rejected := providerRejectionStatus(err)
			if rejected != tc.rejected || status != tc.wantStatus {
				t.Fatalf("providerRejectionStatus = (%d, %t), want (%d, %t)", status, rejected, tc.wantStatus, tc.rejected)
			}
		})
	}

	t.Run("transport loss stays unknown", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		endpoint := server.URL + "/runs"
		server.Close()
		err := newBuildHTTP("x-yunxiao-token", "token").call(context.Background(), http.MethodPost, endpoint, nil, nil)
		var providerError *buildProviderError
		if err == nil || errors.As(err, &providerError) || !strings.Contains(err.Error(), "build provider request failed") {
			t.Fatalf("transport error = %v, want an unclassified request failure", err)
		}
		if _, rejected := providerRejectionStatus(err); rejected {
			t.Fatalf("transport error %v classified as a provider rejection", err)
		}
	})
}

func TestPrebuildSubmitFailureOutcome(t *testing.T) {
	job := &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: 42, State: rivertype.JobStateDiscarded},
		Args:   prebuildJobArgs{EnvironmentUUID: "env-1", WorkspaceUUID: "ws-1"},
	}
	for _, tc := range []struct {
		name           string
		cause          error
		wantState      string
		wantSubmitting bool
		wantUncertain  bool
		wantMessage    string
	}{
		{
			name:        "provider rejection fails without ambiguity",
			cause:       fmt.Errorf("start image build: %w", &buildProviderError{Status: http.StatusUnauthorized}),
			wantState:   "failed",
			wantMessage: prebuildSubmissionRejectedMessage(http.StatusUnauthorized),
		},
		{
			name:           "provider server failure stays unknown",
			cause:          &buildProviderError{Status: http.StatusInternalServerError},
			wantState:      "unknown",
			wantSubmitting: true,
			wantUncertain:  true,
			wantMessage:    errPrebuildSubmissionUnknown.Error(),
		},
		{
			name:           "transport loss stays unknown",
			cause:          errors.New("build provider request failed: connection reset by peer"),
			wantState:      "unknown",
			wantSubmitting: true,
			wantUncertain:  true,
			wantMessage:    errPrebuildSubmissionUnknown.Error(),
		},
		{
			name:           "missing build reference stays unknown",
			cause:          errPrebuildMissingBuildRef,
			wantState:      "unknown",
			wantSubmitting: true,
			wantUncertain:  true,
			wantMessage:    errPrebuildSubmissionUnknown.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := prebuildTask{job: job}
			failure := classifySubmitFailure(tc.cause)
			if failure.definitive != (tc.wantState == "failed") {
				t.Fatalf("classifySubmitFailure(%v) = %+v", tc.cause, failure)
			}
			failure.apply(&task.output)
			if task.output.Submitting != tc.wantSubmitting || task.output.OutcomeUncertain != tc.wantUncertain || task.output.Message != tc.wantMessage {
				t.Fatalf("recorded output = %+v, want submitting=%t uncertain=%t message=%q", task.output, tc.wantSubmitting, tc.wantUncertain, tc.wantMessage)
			}
			if state := task.state(); state != tc.wantState {
				t.Fatalf("reported state = %q, want %q", state, tc.wantState)
			}
			if tc.wantState == "failed" && !strings.Contains(task.output.Message, "401") {
				t.Fatalf("rejection message %q omits the provider status", task.output.Message)
			}
			if tc.wantState == "unknown" && strings.Contains(task.output.Message, "connection reset") {
				t.Fatalf("uncertain message %q leaks transport internals", task.output.Message)
			}
		})
	}
}

func TestPrebuildSubmitFailureLogsProviderStatus(t *testing.T) {
	recorder := &prebuildLogRecorder{}
	svc := &Prebuilds{logger: slog.New(recorder)}
	task := prebuildTask{job: &river.Job[prebuildJobArgs]{
		JobRow: &rivertype.JobRow{ID: 42},
		Args:   prebuildJobArgs{EnvironmentUUID: "env-1", WorkspaceUUID: "ws-1"},
	}}
	for _, tc := range []struct {
		name       string
		cause      error
		wantEvent  string
		wantStatus int
	}{
		{name: "rejection records the provider status", cause: &buildProviderError{Status: http.StatusUnauthorized}, wantEvent: "environment prebuild submission rejected", wantStatus: http.StatusUnauthorized},
		{name: "unknown outcome records the cause", cause: errors.New("build provider request failed: connection reset by peer"), wantEvent: "environment prebuild submission outcome unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder.records = nil
			svc.logSubmitFailure(context.Background(), &task, classifySubmitFailure(tc.cause), tc.cause)
			if len(recorder.records) != 1 {
				t.Fatalf("log records = %+v, want one", recorder.records)
			}
			record := recorder.records[0]
			if record.level != slog.LevelError || record.message != tc.wantEvent {
				t.Fatalf("log record = %+v, want an error event %q", record, tc.wantEvent)
			}
			want := map[string]any{"stage": task.stage(), "job_id": int64(42), "environment_id": "env-1", "workspace_id": "ws-1"}
			for key, value := range want {
				if record.attrs[key] != value {
					t.Fatalf("log record %q = %v, want %v", key, record.attrs[key], value)
				}
			}
			if tc.wantStatus != 0 {
				if record.attrs["status_code"] != int64(tc.wantStatus) {
					t.Fatalf("log status_code = %v, want %d", record.attrs["status_code"], tc.wantStatus)
				}
			} else if _, found := record.attrs["status_code"]; found {
				t.Fatalf("log record for an unknown outcome carries status_code %v", record.attrs["status_code"])
			}
			if cause, ok := record.attrs["error"].(error); !ok || !errors.Is(cause, tc.cause) {
				t.Fatalf("log error attribute = %v, want the submission cause", record.attrs["error"])
			}
		})
	}
}

type prebuildLogRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

type prebuildLogRecorder struct{ records []prebuildLogRecord }

func (r *prebuildLogRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *prebuildLogRecorder) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *prebuildLogRecorder) WithGroup(string) slog.Handler            { return r }

func (r *prebuildLogRecorder) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	r.records = append(r.records, prebuildLogRecord{level: record.Level, message: record.Message, attrs: attrs})
	return nil
}

func TestAliyunFlowLogDownloadCursorRecovery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rangeStatus  int
		contentRange string
		body         string
		offset       int64
		expired      bool
		wantText     string
		wantRequests int
	}{
		{name: "range ignored after truncation", rangeStatus: 200, body: "abc", offset: 5, expired: true, wantRequests: 1},
		{name: "416 after truncation", rangeStatus: 416, contentRange: "bytes */3", body: "abc", offset: 5, expired: true, wantRequests: 2},
		{name: "416 without header after truncation", rangeStatus: 416, body: "abc", offset: 5, expired: true, wantRequests: 2},
		{name: "416 without header at tail", rangeStatus: 416, body: "abc", offset: 3, wantRequests: 2},
		{name: "416 at tail", rangeStatus: 416, contentRange: "bytes */3", body: "abc", offset: 3, wantRequests: 1},
		{name: "range ignored at tail", rangeStatus: 200, body: "abc", offset: 3, wantRequests: 1},
		{name: "416 fallback finds new bytes", rangeStatus: 416, body: "abcdef", offset: 3, wantText: "def", wantRequests: 2},
		{name: "range ignored with new bytes", rangeStatus: 200, body: "abcdef", offset: 3, wantText: "def", wantRequests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Header.Get("Range") != "" {
					if tc.contentRange != "" {
						w.Header().Set("Content-Range", tc.contentRange)
					}
					w.WriteHeader(tc.rangeStatus)
					if tc.rangeStatus == 416 {
						return
					}
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			text, eof, err := readAliyunFlowLogDownload(context.Background(), server.Client(), server.URL, tc.offset)
			if tc.expired {
				var appError *apperr.Error
				if !errors.Is(err, errPrebuildLogCursorExpired) || !errors.As(prebuildError(err), &appError) || appError.Kind != apperr.Conflict {
					t.Fatalf("download error = %v, want expired cursor conflict", err)
				}
			} else if err != nil || !eof || text != tc.wantText {
				t.Fatalf("download = (%q, %t, %v), want (%q, true, nil)", text, eof, err, tc.wantText)
			}
			if requests != tc.wantRequests {
				t.Fatalf("requests = %d, want %d", requests, tc.wantRequests)
			}
		})
	}
}

func TestPrebuildPackagesSnapshot(t *testing.T) {
	packages, err := decodeEnvironmentPackages(json.RawMessage(`{"type":"cloud","packages":{"type":"packages","pip":["b","a"],"npm":null,"go":["example.com/tool@v1"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(prebuildJobArgs{EnvironmentUUID: "env", Packages: resolvedPackagesArg(packages)})
	if err != nil {
		t.Fatal(err)
	}
	task, err := decodePrebuildTask(&rivertype.JobRow{EncodedArgs: encoded, Metadata: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := encodePackagesSnapshot(task.job.Args.Packages)
	want := `{"type":"packages","apt":[],"cargo":[],"gem":[],"go":["example.com/tool@v1"],"npm":[],"pip":["b","a"]}`
	if err != nil || string(snapshot) != want {
		t.Fatalf("snapshot = %s, %v; must match the migration backfill format %s", snapshot, err, want)
	}
	empty, err := encodePackagesSnapshot(resolvedPackagesArg(nil))
	if err != nil || string(empty) != `{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]}` {
		t.Fatalf("empty snapshot = %s, %v", empty, err)
	}
	if decoded, known := decodePackagesSnapshot(empty); !known || decoded != nil {
		t.Fatalf("empty snapshot decoded to %+v, known=%v", decoded, known)
	}
	legacy, err := decodePrebuildTask(&rivertype.JobRow{EncodedArgs: []byte(`{"environment_uuid":"env","dockerfile":"FROM base"}`), Metadata: []byte(`{}`)})
	if err != nil || legacy.job.Args.Packages != nil {
		t.Fatalf("legacy args packages = %+v, %v", legacy.job.Args.Packages, err)
	}
}
