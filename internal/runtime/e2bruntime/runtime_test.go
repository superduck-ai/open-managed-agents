package e2bruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"

	e2b "github.com/superduck-ai/e2b-go-sdk"
)

func TestE2BProviderPropagatesSandboxOperationFailure(t *testing.T) {
	wantErr := errors.New("run failed")
	fake := &fakeSandboxOperations{runErr: wantErr}
	provider := &E2BProvider{
		cfg:       config.Config{E2BRequestTimeout: 17 * time.Second},
		sandboxes: fake,
	}

	err := provider.RunCommand(context.Background(), "sandbox-test", "exit 1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunCommand error = %v, want %v", err, wantErr)
	}
	if fake.runTimeoutMs != 17000 {
		t.Fatalf("RunCommand timeout = %d, want 17000", fake.runTimeoutMs)
	}
}

func TestE2BProviderDelegatesSandboxOperationsWithoutCredentials(t *testing.T) {
	fake := &fakeSandboxOperations{createResult: Sandbox{ID: "sandbox-created"}}
	provider := &E2BProvider{cfg: config.Config{E2BDebug: true}, sandboxes: fake}
	resolved := Resolution{
		Template:            "template-test",
		Metadata:            map[string]string{"environment_id": "env-test"},
		Envs:                map[string]string{"ENV_NODE_VERSION": "22"},
		Timeout:             2 * time.Minute,
		AllowInternetAccess: true,
		Network:             &e2b.SandboxNetworkOpts{AllowOut: []string{"api.example.com"}},
	}

	sandbox, err := provider.Create(context.Background(), db.Environment{}, nil, resolved)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sandbox.ID != "sandbox-created" || fake.createTemplate != "template-test" {
		t.Fatalf("Create result/template = %#v/%q", sandbox, fake.createTemplate)
	}
	if fake.createOpts == nil || fake.createOpts.TimeoutMs == nil || *fake.createOpts.TimeoutMs != 120000 {
		t.Fatalf("Create opts = %#v, want 120000ms timeout", fake.createOpts)
	}
	if !reflect.DeepEqual(fake.createOpts.Metadata, resolved.Metadata) || !reflect.DeepEqual(fake.createOpts.Envs, resolved.Envs) || !reflect.DeepEqual(fake.createOpts.Network, resolved.Network) {
		t.Fatalf("Create opts = %#v, want resolved metadata/env/network", fake.createOpts)
	}
	if fake.createOpts.AllowInternetAccess == nil || !*fake.createOpts.AllowInternetAccess {
		t.Fatalf("Create AllowInternetAccess = %#v, want true", fake.createOpts.AllowInternetAccess)
	}

	payload := []byte("payload")
	if err := provider.WriteFile(context.Background(), sandbox.ID, "/tmp/input", payload); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if fake.writeSandboxID != sandbox.ID || fake.writePath != "/tmp/input" || !reflect.DeepEqual(fake.writeData, payload) {
		t.Fatalf("WriteFile delegation = id=%q path=%q data=%q", fake.writeSandboxID, fake.writePath, fake.writeData)
	}
	if err := provider.RunCommand(context.Background(), sandbox.ID, "echo ready"); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if fake.runSandboxID != sandbox.ID || fake.runCommand != "echo ready" || fake.runTimeoutMs != 60000 {
		t.Fatalf("RunCommand delegation = id=%q command=%q timeout=%d", fake.runSandboxID, fake.runCommand, fake.runTimeoutMs)
	}
	if err := provider.Kill(context.Background(), sandbox.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if fake.killSandboxID != sandbox.ID {
		t.Fatalf("Kill sandbox ID = %q, want %q", fake.killSandboxID, sandbox.ID)
	}
}

type fakeSandboxOperations struct {
	createTemplate string
	createOpts     *e2b.SandboxOpts
	createResult   Sandbox
	createErr      error
	killSandboxID  string
	killErr        error
	writeSandboxID string
	writePath      string
	writeData      []byte
	writeErr       error
	runSandboxID   string
	runCommand     string
	runTimeoutMs   int
	runErr         error
}

func (fake *fakeSandboxOperations) Create(_ context.Context, template string, opts *e2b.SandboxOpts) (Sandbox, error) {
	fake.createTemplate = template
	fake.createOpts = opts
	return fake.createResult, fake.createErr
}

func (fake *fakeSandboxOperations) Kill(_ context.Context, sandboxID string) error {
	fake.killSandboxID = sandboxID
	return fake.killErr
}

func (fake *fakeSandboxOperations) WriteFile(_ context.Context, sandboxID string, filePath string, data []byte) error {
	fake.writeSandboxID = sandboxID
	fake.writePath = filePath
	fake.writeData = append([]byte(nil), data...)
	return fake.writeErr
}

func (fake *fakeSandboxOperations) RunCommand(_ context.Context, sandboxID string, command string, timeoutMs int) error {
	fake.runSandboxID = sandboxID
	fake.runCommand = command
	fake.runTimeoutMs = timeoutMs
	return fake.runErr
}

func TestTruncateCommandOutputNormalizesInvalidUTF8(t *testing.T) {
	got := truncateCommandOutput(string([]byte{'o', 'k', 0xff}))
	if got != "ok\uFFFD" {
		t.Fatalf("truncateCommandOutput = %q, want %q", got, "ok\uFFFD")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncateCommandOutput returned invalid UTF-8: %q", got)
	}
}

func TestTruncateCommandOutputDoesNotSplitUTF8Rune(t *testing.T) {
	got := truncateCommandOutput(strings.Repeat("a", 2047) + "界tail")
	want := strings.Repeat("a", 2047) + "...[truncated]"
	if got != want {
		t.Fatalf("truncateCommandOutput = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncateCommandOutput returned invalid UTF-8: %q", got)
	}
}

func TestTruncateCommandOutputDoesNotSplitEmoji(t *testing.T) {
	got := truncateCommandOutput(strings.Repeat("a", 2046) + "🐥tail")
	want := strings.Repeat("a", 2046) + "...[truncated]"
	if got != want {
		t.Fatalf("truncateCommandOutput = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncateCommandOutput returned invalid UTF-8: %q", got)
	}
}

func TestUniqueStringsTrimsFiltersAndKeepsFirstOrder(t *testing.T) {
	got := uniqueStrings([]string{" b ", "a", "b", "", " a ", "c"})
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueStrings = %#v, want %#v", got, want)
	}
}

func TestSandboxVolumeMountsOnlyIncludeUserData(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
	}{
		{name: "hosted", cfg: config.Config{E2BDomain: "e2b.example.test"}},
		{name: "local endpoint", cfg: config.Config{E2BAPIURL: "http://127.0.0.1:3000"}},
		{name: "debug", cfg: config.Config{E2BDebug: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider(tt.cfg)
			mounts := provider.sandboxVolumeMounts(nil)
			if got := mounts[sandboxUserDataMountPath]; got != sandboxUserDataVolumeName {
				t.Fatalf("mount %s = %v, want %s", sandboxUserDataMountPath, got, sandboxUserDataVolumeName)
			}
			if len(mounts) != 1 {
				t.Fatalf("mounts = %#v, want only user-data", mounts)
			}
		})
	}
}

func TestSandboxVolumeMountsIncludesManagedAgentSkills(t *testing.T) {
	provider := NewProvider(config.Config{})
	work := &db.EnvironmentWork{
		Metadata: json.RawMessage(`{"managed_agent_skills_mount":{"mount_path":"/mnt/skills","volume_name":"managed-agent-skills-test","manifest_sha256":"abc123"}}`),
	}

	mounts := provider.sandboxVolumeMounts(work)
	if got := mounts[sandboxUserDataMountPath]; got != sandboxUserDataVolumeName {
		t.Fatalf("mount %s = %v, want %s", sandboxUserDataMountPath, got, sandboxUserDataVolumeName)
	}
	if got := mounts[SandboxSkillsMountPath]; got != "managed-agent-skills-test" {
		t.Fatalf("mount %s = %v, want managed-agent-skills-test", SandboxSkillsMountPath, got)
	}
	if len(mounts) != 2 {
		t.Fatalf("mounts = %#v, want user-data plus skills", mounts)
	}
}

func TestResolveLimitedNetworkIncludesMCPHostsWhenAllowed(t *testing.T) {
	provider := NewProvider(config.Config{})
	env := db.Environment{
		ExternalID:       "env_test",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true}}`),
		ResolvedTemplate: "template_test",
	}
	work := &db.EnvironmentWork{
		ExternalID: "work_test",
		Metadata:   json.RawMessage(`{"mcp_allowed_hosts":["mcp.notion.com","api.githubcopilot.com","mcp.notion.com"]}`),
	}

	resolution, err := provider.Resolve(env, work)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.AllowInternetAccess {
		t.Fatalf("limited network should disable unrestricted internet")
	}
	if resolution.Network == nil {
		t.Fatalf("expected network options")
	}
	want := []string{"api.example.com", "mcp.notion.com", "api.githubcopilot.com"}
	if !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network.AllowOut, want)
	}
}

func TestSkillMountVolumeNameUsesFullManifestHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	got := skillMountVolumeName(hash)
	want := skillMountVolumePrefix + hash
	if got != want {
		t.Fatalf("skillMountVolumeName = %q, want %q", got, want)
	}
}

func TestPrepareSkillMountReusesOnlyMatchingReadyMarker(t *testing.T) {
	archive := []byte("skill archive")
	sum := sha256.Sum256(archive)
	sha := fmt.Sprintf("%x", sum[:])
	runtimeSkills := []skillsapi.RuntimeSkill{{
		Source:    "custom",
		SkillID:   "skill_1",
		Version:   "1",
		Directory: "skill-one",
		SHA256:    sha,
		SizeBytes: int64(len(archive)),
		Archive:   archive,
	}}
	_, _, manifestSHA256, err := skillsapi.BuildMountManifest(runtimeSkills)
	if err != nil {
		t.Fatalf("BuildMountManifest: %v", err)
	}
	volumeName := skillMountVolumeName(manifestSHA256)

	t.Run("matching ready marker reuses without writes", func(t *testing.T) {
		var writes []string
		server := newFakeE2BVolumeServer(t, volumeName, manifestSHA256, &writes)
		defer server.Close()

		provider := NewProvider(config.Config{
			E2BAPIKey: "e2b_0000000000000000000000000000000000000000",
			E2BAPIURL: server.URL,
		})
		metadataOnly := append([]skillsapi.RuntimeSkill(nil), runtimeSkills...)
		metadataOnly[0].Archive = nil
		mount, err := provider.PrepareSkillMount(context.Background(), metadataOnly)
		if err != nil {
			t.Fatalf("PrepareSkillMount: %v", err)
		}
		if mount.VolumeName != volumeName || mount.ManifestSHA256 != manifestSHA256 {
			t.Fatalf("mount = %#v, want volume=%s manifest=%s", mount, volumeName, manifestSHA256)
		}
		if len(writes) != 0 {
			t.Fatalf("writes = %#v, want none for matching ready marker", writes)
		}
	})

	t.Run("mismatched ready marker rewrites volume", func(t *testing.T) {
		var writes []string
		server := newFakeE2BVolumeServer(t, volumeName, "stale-ready-marker", &writes)
		defer server.Close()

		provider := NewProvider(config.Config{
			E2BAPIKey: "e2b_0000000000000000000000000000000000000000",
			E2BAPIURL: server.URL,
		})
		mount, err := provider.PrepareSkillMount(context.Background(), runtimeSkills)
		if err != nil {
			t.Fatalf("PrepareSkillMount: %v", err)
		}
		if mount.VolumeName != volumeName || mount.ManifestSHA256 != manifestSHA256 {
			t.Fatalf("mount = %#v, want volume=%s manifest=%s", mount, volumeName, manifestSHA256)
		}
		if len(writes) != 3 {
			t.Fatalf("writes = %#v, want manifest, archive, ready", writes)
		}
		if writes[len(writes)-1] != skillMountReadyPath+"="+manifestSHA256+"\n" {
			t.Fatalf("ready write = %q, want manifest hash", writes[len(writes)-1])
		}
	})
}

func newFakeE2BVolumeServer(t *testing.T, volumeName string, readyMarker string, writes *[]string) *httptest.Server {
	t.Helper()
	const volumeID = "vol-skills-test"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/volumes":
			_, _ = w.Write([]byte(`[{"volumeID":"` + volumeID + `","name":"` + volumeName + `"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/"+volumeID:
			_, _ = w.Write([]byte(`{"volumeID":"` + volumeID + `","name":"` + volumeName + `","token":"volume-token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumecontent/"+volumeID+"/path" && r.URL.Query().Get("path") == skillMountReadyPath:
			_, _ = w.Write([]byte(`{"type":"file","name":"` + skillMountReadyPath + `","path":"` + skillMountReadyPath + `","size":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumecontent/"+volumeID+"/file" && r.URL.Query().Get("path") == skillMountReadyPath:
			_, _ = w.Write([]byte(readyMarker))
		case r.Method == http.MethodPut && r.URL.Path == "/volumecontent/"+volumeID+"/file":
			path := r.URL.Query().Get("path")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read write body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			*writes = append(*writes, path+"="+string(data))
			_, _ = w.Write([]byte(`{"type":"file","name":"` + path + `","path":"` + path + `","size":1}`))
		default:
			t.Errorf("unexpected E2B request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
