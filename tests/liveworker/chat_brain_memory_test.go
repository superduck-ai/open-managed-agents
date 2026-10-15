package liveworker

import (
	"context"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestChatBrainMemory(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	writable := createMountedMemoryStore(t, e, "verified-write", "shared notes")
	readonly := createMountedMemoryStore(t, e, "verified-read", "team reference")
	const marker = "HOST_MEMORY_BODY_ONLY_FROM_READ"
	e.request(t, "POST", "/v1/memory_stores/"+readonly+"/memories", e.apiKey, map[string]string{"path": "/seed.md", "content": marker}, 200)
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"model":   map[string]string{"id": "claude-sonnet-4-6"},
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_ask"}}}},
	}, 200)
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	node := startBrainNode(t, e, cfg, "memory-loading")
	defer node.stop(t)
	f := createMemoryMountSession(t, e, writable, readonly, "read_write")
	client := chatSDK(e)
	previous := ""
	for turn, decision := range []string{"deny", "text", "allow", "allow"} {
		location := "/mnt/memory/verified-read/seed.md"
		model := fileCatalogModel(t, &brainSkillModel{preview: make(chan struct{}, 4)}, uuid.NewString(), decision, location, marker, []string{
			"attached_memory_stores", "verified-write", "/mnt/memory/verified-write", "read_write", "shared notes", "preserve exact bytes",
			"verified-read", "/mnt/memory/verified-read", "read_only", "team reference", "Read relevant documents on demand",
		})
		configureChatModel(t, e, model.URL)
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		events, closeStream := connectChatStream(t, ctx, f)
		submitChat(t, f, "Use attached memory when needed.")
		if decision != "text" {
			toolID := waitNewFilePermission(t, &client, f.session.ExternalID, previous)
			previous = toolID
			if turn < 3 && provider.created.Load() != 0 {
				t.Fatal("Memory discovery or unapproved Read allocated a sandbox")
			}
			sendChatSDK(t, &client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{
				Type: "user.tool_confirmation", ToolUseID: toolID, Result: anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult(decision),
			}})
		}
		receiveChatAnswer(t, ctx, events, func() { model.preview <- struct{}{} })
		f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
		requireOK(t, err)
		waitChatIdle(t, f, turn+1)
		closeStream()
		cancel()
		if decision != "text" {
			assertFilePermissionHistory(t, e, f, previous, decision, marker)
		}
		if turn < 2 {
			if provider.created.Load() != 0 {
				t.Fatal("Memory metadata or denied Read waited for a sandbox")
			}
		} else {
			if provider.created.Load() != 1 {
				t.Fatal("Memory Read did not reuse exactly one sandbox")
			}
			if turn == 2 {
				registerPublicSandboxCleanup(t, e, f)
			}
		}
	}
	sandbox := waitMemorySandbox(t, e, f)
	memorySandboxCommand(t, provider, sandbox, "test ! -e /tmp/rclone-mount-config.json", nil, 0)
	memorySandboxRead(t, provider, sandbox, "/mnt/memory/verified-read/seed.md", marker)
	result, err := provider.RunCommand(t.Context(), sandbox, e2bruntime.CommandRequest{Command: "cat > /mnt/memory/verified-read/seed.md", Stdin: []byte("denied"), Timeout: 15 * time.Second})
	requireOK(t, err)
	if result.ExitCode == 0 {
		t.Fatal("readonly Memory mount accepted a write")
	}
	memorySandboxRead(t, provider, sandbox, "/mnt/memory/verified-read/seed.md", marker)
	chatProof(t, started, "brain_memory_loaded")
}
