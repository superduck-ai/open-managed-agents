package environments

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/riverqueue/river/rivertype"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestTemplateConfigRejectsInvalidFields(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   string
	}{
		{"script type", `{"init_script":1}`, "config.init_script must be a string"},
		{"script size", `{"init_script":"` + strings.Repeat("a", maxInitScriptBytes+1) + `"}`, "64 KiB"},
		{"script NUL", `{"init_script":"echo \u0000"}`, "NUL"},
		{"script credentials", `{"init_script":"git clone https://user:token@git.example.com/repo"}`, "URL credentials"},
		{"environment type", `{"environment":{"A":1}}`, "config.environment must be an object"},
		{"environment name", `{"environment":{"1A":"x"}}`, "valid environment variable names"},
		{"environment reserved", `{"environment":{"anthropic_api_key":"x"}}`, "reserved ANTHROPIC_ prefix"},
		{"environment oma reserved", `{"environment":{"OMA_PACKAGE":"x"}}`, "reserved OMA_ prefix"},
		{"resources unknown", `{"resources":{"gpu":1}}`, "config.resources must be an object"},
		{"resources negative", `{"resources":{"cpu":-1}}`, "config.resources must be an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfigForCreate(json.RawMessage(tc.config))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTemplateConfigRejectsTooManyEnvironmentVariables(t *testing.T) {
	variables := map[string]string{}
	for i := range maxEnvironmentVariables + 1 {
		variables["V"+strings.Repeat("X", i)] = "1"
	}
	raw, _ := json.Marshal(map[string]any{"environment": variables})
	if _, err := normalizeConfigForCreate(raw); err == nil {
		t.Fatal("too many environment variables accepted")
	}
}

func TestTemplateConfigNormalizesAndClearsFields(t *testing.T) {
	created, err := normalizeConfigForCreate(json.RawMessage(`{"init_script":"  apt-get update \n","environment":{"MODE":"test"},"resources":{"cpu":2000,"disk":40}}`))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(created, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["init_script"] != "apt-get update" {
		t.Fatalf("init_script = %#v", stored["init_script"])
	}
	if got := stored["resources"].(map[string]any); got["cpu"] != float64(2000) || got["disk"] != float64(40) || got["memory"] != nil {
		t.Fatalf("resources = %#v", got)
	}
	unchanged, err := normalizeConfigForUpdate(created, json.RawMessage(`{"packages":{"npm":["typescript"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	fields, err := decodeTemplateConfigFields(unchanged)
	if err != nil || fields.InitScript != "apt-get update" || fields.Environment["MODE"] != "test" || fields.Resources.CPU != 2000 {
		t.Fatalf("partial update dropped template fields: %+v %v", fields, err)
	}
	cleared, err := normalizeConfigForUpdate(created, json.RawMessage(`{"init_script":null,"environment":{},"resources":null}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"init_script", "environment", "resources"} {
		if strings.Contains(string(cleared), `"`+key+`"`) {
			t.Fatalf("%s not cleared: %s", key, cleared)
		}
	}
}

func TestTemplateSpecPolicy(t *testing.T) {
	enabled := config.EnvironmentPrebuildConfig{Enabled: true, Template: config.TemplateBuildConfig{MaxResources: config.TemplateResourceLimit{CPU: 4000, Memory: 8192}}}
	for _, tc := range []struct {
		name     string
		config   string
		prebuild config.EnvironmentPrebuildConfig
		want     string
	}{
		{"script requires prebuild", `{"type":"cloud","init_script":"make"}`, config.EnvironmentPrebuildConfig{}, "require environment prebuilds"},
		{"resources require prebuild", `{"type":"cloud","resources":{"cpu":1000}}`, config.EnvironmentPrebuildConfig{}, "require environment prebuilds"},
		{"disabled dimension", `{"type":"cloud","resources":{"disk":40}}`, enabled, "disk cannot be customized"},
		{"over limit", `{"type":"cloud","resources":{"memory":16384}}`, enabled, "memory must not exceed 8192"},
		{"within limits", `{"type":"cloud","init_script":"make","resources":{"cpu":4000,"memory":2048}}`, enabled, ""},
		{"environment only", `{"type":"cloud","environment":{"A":"b"}}`, config.EnvironmentPrebuildConfig{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTemplateSpecPolicy(json.RawMessage(tc.config), tc.prebuild)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTemplateBuildSpecEquality(t *testing.T) {
	decode := func(raw string) *environmentBuildSpec {
		spec, err := decodeEnvironmentBuildSpec(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	base := decode(`{"type":"cloud","packages":{"npm":["a"]},"init_script":"make","resources":{"cpu":1000}}`)
	for _, tc := range []struct {
		name   string
		config string
		same   bool
	}{
		{"script changed", `{"type":"cloud","packages":{"npm":["a"]},"init_script":"make all","resources":{"cpu":1000}}`, false},
		{"resources changed", `{"type":"cloud","packages":{"npm":["a"]},"init_script":"make","resources":{"cpu":2000}}`, false},
		{"packages changed", `{"type":"cloud","packages":{"npm":["b"]},"init_script":"make","resources":{"cpu":1000}}`, false},
		{"environment ignored", `{"type":"cloud","packages":{"npm":["a"]},"init_script":"make","resources":{"cpu":1000},"environment":{"A":"b"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if sameBuildSpec(base, decode(tc.config)) != tc.same {
				t.Fatalf("same = %v, want %v", !tc.same, tc.same)
			}
		})
	}
	if decode(`{"type":"cloud","environment":{"A":"b"}}`) != nil {
		t.Fatal("environment variables alone must not require a build")
	}
}

func TestTemplateDockerfileAppendsInitScriptAfterPackages(t *testing.T) {
	spec, err := decodeEnvironmentBuildSpec(json.RawMessage(`{"type":"cloud","packages":{"npm":["@example/demo-cli"]},"init_script":"echo \"$HOME\" && make"}`))
	if err != nil {
		t.Fatal(err)
	}
	file := templateDockerfile("registry.example.com/base:1", spec, config.EnvironmentPackageConfig{})
	npm := strings.Index(file, `RUN ["npm","install","--global","--","@example/demo-cli"]`)
	encoded, _ := json.Marshal([]string{"bash", "-eo", "pipefail", "-c", `echo "$HOME" && make`})
	script := strings.Index(file, "RUN "+string(encoded))
	if npm < 0 || script < npm {
		t.Fatalf("unexpected Dockerfile:\n%s", file)
	}
	scriptOnly, err := decodeEnvironmentBuildSpec(json.RawMessage(`{"type":"cloud","init_script":"make"}`))
	if err != nil || !scriptOnly.needsImage() || !strings.HasSuffix(templateDockerfile("registry.example.com/base:1", scriptOnly, config.EnvironmentPackageConfig{}), `RUN ["bash","-eo","pipefail","-c","make"]`+"\n") {
		t.Fatalf("script-only spec = %+v, %v", scriptOnly, err)
	}
}

func TestTemplateInputUsesEnvironmentResources(t *testing.T) {
	svc := &Prebuilds{cfg: config.EnvironmentPrebuildConfig{Template: config.TemplateBuildConfig{CPU: 1000, Memory: 2048, DiskSize: "20G"}}}
	if got := svc.templateInput(templateResources{}); got != (templateBuildInput{CPU: 1000, Memory: 2048, DiskSize: "20G"}) {
		t.Fatalf("default input = %+v", got)
	}
	if got := svc.templateInput(templateResources{CPU: 4000, Disk: 60}); got != (templateBuildInput{CPU: 4000, Memory: 2048, DiskSize: "60G"}) {
		t.Fatalf("custom input = %+v", got)
	}
}

func TestResourceOnlyPrebuildStartsAtTemplateStage(t *testing.T) {
	args, _ := json.Marshal(prebuildJobArgs{EnvironmentUUID: "env", BaseImage: "registry.example.com/base:1", Template: &templateBuildInput{CPU: 4000, DiskSize: "20G"}})
	task, err := decodePrebuildTask(&rivertype.JobRow{EncodedArgs: args, Metadata: []byte(`{}`), State: rivertype.JobStateAvailable})
	if err != nil {
		t.Fatal(err)
	}
	if task.stage() != "template" || task.output.ImageRef != "registry.example.com/base:1" || task.state() != "queued" {
		t.Fatalf("stage=%s image=%q state=%s", task.stage(), task.output.ImageRef, task.state())
	}
	if response := (&Prebuilds{}).currentPrebuildResponse(db.Environment{}, task); !response.ImageSkipped || response.HasImageLogs {
		t.Fatalf("response = %+v", response)
	}
}

func TestResolveEnvironmentSandboxRequiresPrebuiltTemplate(t *testing.T) {
	jobID := int64(7)
	runner := &Runner{provider: e2bruntime.NewProvider(config.E2BConfig{Template: "base"})}
	for _, tc := range []struct {
		name string
		env  db.Environment
	}{
		{"script without build", db.Environment{Config: json.RawMessage(`{"type":"cloud","init_script":"make"}`), ResolvedTemplate: "base"}},
		{"resources while building", db.Environment{Config: json.RawMessage(`{"type":"cloud","resources":{"cpu":2000}}`), BuildJobID: &jobID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := runner.resolveEnvironmentSandbox(tc.env, nil); !errors.Is(err, errEnvironmentTemplateNotReady) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	env := db.Environment{
		ExternalID:       "env_1",
		Config:           json.RawMessage(`{"type":"cloud","init_script":"make","environment":{"MODE":"test"}}`),
		BuildJobID:       &jobID,
		ResolvedTemplate: "built",
	}
	resolution, preinstalled, err := runner.resolveEnvironmentSandbox(env, nil)
	if err != nil || !preinstalled || resolution.Template != "built" {
		t.Fatalf("resolution = %+v preinstalled=%v err=%v", resolution, preinstalled, err)
	}
	if resolution.Envs["MODE"] != "test" || resolution.Envs["ANTHROPIC_ENVIRONMENT_ID"] != "env_1" {
		t.Fatalf("envs = %+v", resolution.Envs)
	}
}

func TestPlatformResponseIncludesTemplateFields(t *testing.T) {
	var response map[string]any
	raw := platformEnvironmentConfigForResponse(json.RawMessage(`{"type":"cloud","init_script":"make","environment":{"A":"b"},"resources":{"memory":4096}}`))
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response["init_script"] != "make" || response["environment"].(map[string]any)["A"] != "b" || response["resources"].(map[string]any)["memory"] != float64(4096) {
		t.Fatalf("response = %s", raw)
	}
	if err := json.Unmarshal(platformEnvironmentConfigForResponse(json.RawMessage(`{"type":"cloud"}`)), &response); err != nil || response["resources"] != nil {
		t.Fatalf("empty resources = %#v, %v", response["resources"], err)
	}
}

func TestPlatformResponseNetworkingDefaultsToUnrestricted(t *testing.T) {
	for raw, want := range map[string]string{
		`not json`:         "unrestricted",
		`{"type":"cloud"}`: "unrestricted",
		`{"type":"cloud","networking":{"type":"limited"}}`: "limited",
	} {
		var response struct {
			Networking struct {
				Type string `json:"type"`
			} `json:"networking"`
		}
		if err := json.Unmarshal(platformEnvironmentConfigForResponse(json.RawMessage(raw)), &response); err != nil || response.Networking.Type != want {
			t.Fatalf("networking type for %s = %q, %v, want %q", raw, response.Networking.Type, err, want)
		}
	}
}
