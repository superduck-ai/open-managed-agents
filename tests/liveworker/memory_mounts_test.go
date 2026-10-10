package liveworker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestVerifyMemoryMounts(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	writable := createMountedMemoryStore(t, e, "verified-write", "shared notes\nnext line")
	readonly := createMountedMemoryStore(t, e, "verified-read", "")
	e.request(t, "POST", "/v1/memory_stores/"+writable+"/memories", e.apiKey, map[string]string{"path": "/seed.md", "content": "seed\n中文"}, 200)
	e.request(t, "POST", "/v1/memory_stores/"+readonly+"/memories", e.apiKey, map[string]string{"path": "/seed.md", "content": "readonly seed\n"}, 200)
	provider, stop := startPublicRunner(t, e)
	defer stop()
	provider.failMemoryMarkdown.Store(true)
	failed := createMemoryMountSession(t, e, writable, readonly, "read_write")
	waitRealWorker(t, "mount setup failure stopped work", func() bool {
		work, err := e.database.GetLatestEnvironmentWorkForSession(t.Context(), e.key.WorkspaceUUID.String(), e.environment.ExternalID, failed.session.UUID)
		requireOK(t, err)
		return work.State == "stopped"
	})
	if provider.created.Load() != 1 {
		t.Fatal("setup failed before allocating a real sandbox")
	}
	waitRealWorker(t, "failed mount sandbox removed", func() bool {
		output, err := exec.CommandContext(t.Context(), "docker", "ps", "-aq", "--filter", "name=^/"+provider.name+"-1$").Output()
		requireOK(t, err)
		return len(output) == 0
	})
	_, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), failed.session.ExternalID)
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("failed setup exposed CodeSession: %v", err)
	}
	chatProof(t, started, "mount_failure_cleaned")
	first := createMemoryMountSession(t, e, writable, readonly, "read_write")
	sandbox := waitMemorySandbox(t, e, first)
	registerPublicSandboxCleanup(t, e, first)
	for _, path := range []string{"/mnt/session/uploads", "/mnt/user-data/outputs", "/mnt/transcripts", "/mnt/user-data/tool_results", "/root/.claude/skills", "/mnt/memory/verified-write", "/mnt/memory/verified-read"} {
		memorySandboxCommand(t, provider, sandbox, "mountpoint -q '"+path+"'", nil, 0)
	}
	memorySandboxRead(t, provider, sandbox, "/mnt/memory/verified-write/seed.md", "seed\n中文")
	memorySandboxRead(t, provider, sandbox, "/mnt/memory/verified-read/seed.md", "readonly seed\n")
	result, err := provider.RunCommand(t.Context(), sandbox, e2bruntime.CommandRequest{Command: "cat > /mnt/memory/verified-read/seed.md", Stdin: []byte("denied"), Timeout: 15 * time.Second})
	requireOK(t, err)
	if result.ExitCode == 0 {
		t.Fatal("read-only FUSE mount accepted a write")
	}
	memorySandboxRead(t, provider, sandbox, "/mnt/memory/verified-read/seed.md", "readonly seed\n")
	markdown := "<!-- oma-stores -->\n- [verified-write](/mnt/memory/verified-write) rw — shared notes next line。preserve exact bytes\n- [verified-read](/mnt/memory/verified-read) ro\n"
	memorySandboxMarkdown(t, provider, sandbox, markdown)
	exists, err := provider.FileExists(t.Context(), sandbox, "/tmp/rclone-mount-config.json")
	requireOK(t, err)
	if exists {
		t.Fatal("mount config containing tokens remains on disk")
	}
	chatProof(t, started, "memory_mounts_ready")
	const content = "written through real FUSE\n中文\n"
	requireOK(t, provider.WriteFile(t.Context(), sandbox, "/mnt/memory/verified-write/from-session.md", []byte(content)))
	var head db.Memory
	waitRealWorker(t, "mounted write durable in metadata", func() bool {
		var found bool
		head, found, err = e.database.GetMemoryByPath(t.Context(), e.key.WorkspaceUUID.String(), writable, "/from-session.md")
		requireOK(t, err)
		return found && head.ContentSizeBytes == int64(len(content))
	})
	object, err := e.objects.Open(t.Context(), head.S3Key, nil)
	requireOK(t, err)
	bytes, err := io.ReadAll(object.Body)
	requireOK(t, err)
	requireOK(t, object.Body.Close())
	if string(bytes) != content {
		t.Fatal("mounted write object bytes differ")
	}
	versions, _, err := e.database.ListMemoryVersionsPage(t.Context(), db.ListMemoryVersionsPageParams{WorkspaceUUID: e.key.WorkspaceUUID.String(), MemoryStoreExternalID: writable, MemoryExternalID: head.ExternalID, Limit: 10})
	requireOK(t, err)
	if len(versions) != 1 || versions[0].CreatedBy.Type != "session_actor" || versions[0].CreatedBy.SessionID != first.session.ExternalID {
		t.Fatalf("mounted write actor or versions differ: %+v", versions)
	}
	chatProof(t, started, "mounted_write_persisted")
	requireOK(t, provider.WriteFile(t.Context(), sandbox, "/mnt/memory/local-only.txt", []byte("sandbox local")))
	requireOK(t, provider.WriteFile(t.Context(), sandbox, "/mnt/memory/MEMORY.md", []byte("local edit")))
	requireOK(t, provider.Kill(t.Context(), sandbox))
	second := createMemoryMountSession(t, e, writable, readonly, "read_only")
	fresh := waitMemorySandbox(t, e, second)
	registerPublicSandboxCleanup(t, e, second)
	if fresh == sandbox {
		t.Fatal("new session reused destroyed sandbox")
	}
	memorySandboxRead(t, provider, fresh, "/mnt/memory/verified-write/from-session.md", content)
	exists, err = provider.FileExists(t.Context(), fresh, "/mnt/memory/local-only.txt")
	requireOK(t, err)
	if exists {
		t.Fatal("sandbox-local root survived a new session")
	}
	memorySandboxMarkdown(t, provider, fresh, strings.Replace(markdown, ") rw", ") ro", 1))
	chatProof(t, started, "sandbox_lifecycle_matches")
}

func createMountedMemoryStore(t *testing.T, e *liveEnv, name, description string) string {
	t.Helper()
	var result struct {
		ID string `json:"id"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/memory_stores", e.apiKey, map[string]string{"name": name, "description": description}, 200), &result))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "DELETE", "/v1/memory_stores/"+result.ID, e.apiKey, nil, 200)
	})
	return result.ID
}

func createMemoryMountSession(t *testing.T, e *liveEnv, writable, readonly, access string) *liveSession {
	t.Helper()
	var result struct {
		ID string `json:"id"`
	}
	body := map[string]any{"agent": e.agent.ExternalID, "environment_id": e.environment.ExternalID, "resources": []map[string]string{{"type": "memory_store", "memory_store_id": writable, "access": access, "instructions": "preserve exact bytes"}, {"type": "memory_store", "memory_store_id": readonly, "access": "read_only"}}}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/sessions", e.apiKey, body, 200), &result))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "DELETE", "/v1/sessions/"+result.ID, e.apiKey, nil, 200)
	})
	record, _, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), result.ID)
	requireOK(t, err)
	return &liveSession{env: e, session: record}
}

func waitMemorySandbox(t *testing.T, e *liveEnv, f *liveSession) string {
	t.Helper()
	var sandbox db.EnvironmentSandbox
	waitRealWorker(t, "public memory sandbox ready", func() bool {
		var err error
		f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
		if errors.Is(err, db.ErrNotFound) {
			return false
		}
		requireOK(t, err)
		sandbox, err = e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), f.code.ExternalID)
		if errors.Is(err, db.ErrNotFound) {
			return false
		}
		requireOK(t, err)
		return sandbox.State == "running" && sandbox.ProviderSandboxID != nil
	})
	return *sandbox.ProviderSandboxID
}

func memorySandboxCommand(t *testing.T, p *chatDockerProvider, id, command string, stdin []byte, want int) {
	t.Helper()
	result, err := p.RunCommand(t.Context(), id, e2bruntime.CommandRequest{Command: command, Stdin: stdin, Timeout: 15 * time.Second})
	requireOK(t, err)
	if result.ExitCode != want {
		t.Fatalf("sandbox command %q exit=%d stderr=%s", command, result.ExitCode, result.Stderr)
	}
}

func memorySandboxRead(t *testing.T, p *chatDockerProvider, id, path, expected string) {
	t.Helper()
	result, err := p.RunCommand(t.Context(), id, e2bruntime.CommandRequest{Command: "cat '" + path + "'", Timeout: 15 * time.Second})
	requireOK(t, err)
	if result.ExitCode != 0 || string(result.Stdout) != expected {
		t.Fatalf("mounted read %s exit=%d bytes=%q want=%q stderr=%s", path, result.ExitCode, result.Stdout, expected, result.Stderr)
	}
}

func memorySandboxMarkdown(t *testing.T, p *chatDockerProvider, id, expected string) {
	t.Helper()
	result, err := p.RunCommand(t.Context(), id, e2bruntime.CommandRequest{Command: "cat /mnt/memory/MEMORY.md", Timeout: 15 * time.Second})
	requireOK(t, err)
	actual := strings.Split(string(result.Stdout), "\n")
	wanted := strings.Split(expected, "\n")
	if result.ExitCode != 0 || len(actual) != 4 || actual[0] != wanted[0] || actual[3] != "" {
		t.Fatalf("invalid MEMORY.md %q", result.Stdout)
	}
	slices.Sort(actual[1:3])
	slices.Sort(wanted[1:3])
	if !slices.Equal(actual, wanted) {
		t.Fatalf("MEMORY.md content=%q want=%q", result.Stdout, expected)
	}
}
