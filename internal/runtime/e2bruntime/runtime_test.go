package e2bruntime

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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
