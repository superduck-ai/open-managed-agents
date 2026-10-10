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
			c := NewPrebuilds(nil, cfg, nil).templates.(*cubeSandboxTemplateBuilder)
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
			current := db.Environment{Config: json.RawMessage(tc.config), BuildJobID: tc.jobID, ResolvedTemplate: tc.storedTemplate}
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
		Config:           json.RawMessage(`{"type":"cloud","packages":{"apt":"vim"}}`),
		BuildJobID:       &jobID,
		ResolvedTemplate: "old-template",
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
	if next.BuildJobID != nil || next.ResolvedTemplate != "base-b" {
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
