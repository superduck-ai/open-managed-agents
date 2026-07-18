package e2bruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

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

func TestResolveUsesManagedAgentSandboxTagByDefault(t *testing.T) {
	resolution, err := NewProvider(config.Config{}).Resolve(db.Environment{
		ExternalID:  "env_default_template",
		WorkspaceID: 42,
		Config:      json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`),
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Template != config.DefaultE2BTemplate {
		t.Fatalf("template = %q, want %q", resolution.Template, config.DefaultE2BTemplate)
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

func TestResolveLimitedNetworkIncludesCargoStaticCDNWhenPackageManagersAllowed(t *testing.T) {
	provider := NewProvider(config.Config{})
	resolution, err := provider.Resolve(db.Environment{
		ExternalID:       "env_packages",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allow_package_managers":true}}`),
		ResolvedTemplate: "template_test",
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Network == nil {
		t.Fatal("expected limited network options")
	}
	allowedHosts, ok := resolution.Network.AllowOut.([]string)
	if !ok {
		t.Fatalf("AllowOut = %#v, want []string", resolution.Network.AllowOut)
	}
	if !slices.Contains(allowedHosts, "static.crates.io") {
		t.Fatalf("AllowOut = %#v, want Cargo static CDN", allowedHosts)
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
