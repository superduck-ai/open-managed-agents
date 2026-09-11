package environments

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestManagedAgentRuntimeResourcesCollectMemoryMountsWithoutSources(t *testing.T) {
	t.Parallel()
	resources := []db.SessionResource{
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":"https://github.com/acme/widgets","mount_path":"/workspace/widgets"}`),
		},
		memoryStoreSessionResource(
			"user-preferences",
			"user-preferences",
			sessionresource.MemoryAccessReadWrite,
			"用户的饮食与语言偏好",
			"问饮食或语言先读此目录；有新偏好就更新对应文件。",
		),
		memoryStoreSessionResource(
			"team-playbook",
			"team-playbook",
			sessionresource.MemoryAccessReadOnly,
			"团队规范，只读，不得修改。",
			"",
		),
	}

	resolved := resolveManagedAgentRuntimeResources(resources)
	sources := managedAgentRuntimeSourceValues(t, resolved.sources)
	wantSources := []any{
		map[string]any{
			"type":       "git_repository",
			"url":        "https://github.com/acme/widgets",
			"mount_path": "/workspace/widgets",
		},
	}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", sources, wantSources)
	}
	if len(resolved.memoryMounts) != 2 {
		t.Fatalf("memory mounts = %#v, want 2", resolved.memoryMounts)
	}
	if resolved.memoryMounts[0].Slug != "user-preferences" ||
		resolved.memoryMounts[0].Access != sessionresource.MemoryAccessReadWrite ||
		resolved.memoryMounts[0].MountPath != "/mnt/memory/user-preferences" {
		t.Fatalf("rw mount = %+v", resolved.memoryMounts[0])
	}
	if resolved.memoryMounts[1].Slug != "team-playbook" ||
		resolved.memoryMounts[1].Access != sessionresource.MemoryAccessReadOnly {
		t.Fatalf("ro mount = %+v", resolved.memoryMounts[1])
	}
}

func TestManagedAgentRuntimeResourcesReportUnparseableMemorySnapshots(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		payload string
	}{
		{name: "unknown access", payload: `{"access":"admin","mount_path":"/mnt/memory/user-preferences"}`},
		{name: "empty mount path", payload: `{"access":"rw","mount_path":""}`},
		{name: "nested mount path", payload: `{"access":"rw","mount_path":"/mnt/memory/team/playbook"}`},
		{name: "malformed json", payload: `{"access":`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			resolved := resolveManagedAgentRuntimeResources([]db.SessionResource{{
				ResourceType: sessionresource.MemoryStoreType,
				ExternalID:   "sesrsc_broken",
				Payload:      json.RawMessage(testCase.payload),
			}})
			if len(resolved.memoryMounts) != 0 {
				t.Fatalf("memory mounts = %#v, want none", resolved.memoryMounts)
			}
			want := []string{"sesrsc_broken"}
			if !reflect.DeepEqual(resolved.invalidMemoryResources, want) {
				t.Fatalf("invalid memory resources = %#v, want %#v", resolved.invalidMemoryResources, want)
			}
		})
	}
}

func TestBuildRcloneMultimountConfigIncludesMemoryStores(t *testing.T) {
	t.Parallel()
	const (
		filesystemID = "claude_chat_test"
		serviceURL   = "http://host.docker.internal:38080/"
		readWrite    = "rw-token"
		readonly     = "ro-token"
	)
	got := buildRcloneMultimountConfig(filesystemID, serviceURL, readWrite, readonly, []memoryRuntimeMount{
		{
			Name:      "user-preferences",
			Access:    sessionresource.MemoryAccessReadWrite,
			MountPath: "/mnt/memory/user-preferences",
			Slug:      "user-preferences",
		},
		{
			Name:      "team-playbook",
			Access:    sessionresource.MemoryAccessReadOnly,
			MountPath: "/mnt/memory/team-playbook",
			Slug:      "team-playbook",
		},
	})
	if len(got.Mounts) != 7 {
		t.Fatalf("mount count = %d, want 7", len(got.Mounts))
	}
	rw := got.Mounts[5]
	if rw.Source != "/memory/user-preferences" || rw.Destination != "/mnt/memory/user-preferences" ||
		rw.Readonly || rw.AuthToken != readWrite || rw.CacheDurationSeconds != 1 {
		t.Fatalf("rw memory mount = %+v", rw)
	}
	ro := got.Mounts[6]
	if ro.Source != "/memory/team-playbook" || ro.Destination != "/mnt/memory/team-playbook" ||
		!ro.Readonly || ro.AuthToken != readonly || ro.CacheDurationSeconds != 1 {
		t.Fatalf("ro memory mount = %+v", ro)
	}
	for _, mount := range got.Mounts {
		if mount.Source == "/memory" || mount.Destination == sessionresource.MemoryMountRoot {
			t.Fatalf("parent /mnt/memory must not be a filestore mount: %+v", mount)
		}
	}
}

func TestRenderMemoryMarkdownMatchesDesignContract(t *testing.T) {
	t.Parallel()
	instructions := strings.Repeat("字", sessionresource.MaxMemoryInstructionsRunes)
	got := renderMemoryMarkdown([]memoryRuntimeMount{
		{
			Name:         "user-preferences",
			Description:  "用户的饮食与语言偏好",
			Instructions: "问饮食或语言先读此目录；有新偏好就更新对应文件。",
			Access:       sessionresource.MemoryAccessReadWrite,
			MountPath:    "/mnt/memory/user-preferences",
		},
		{
			Name:         "oma-project",
			Description:  "架构决策",
			Instructions: "开始任务前先读；新结论写入 decisions/。",
			Access:       sessionresource.MemoryAccessReadWrite,
			MountPath:    "/mnt/memory/oma-project",
		},
		{
			Name:         "team-playbook",
			Description:  "团队规范，只读，不得修改",
			Instructions: "",
			Access:       sessionresource.MemoryAccessReadOnly,
			MountPath:    "/mnt/memory/team-playbook",
		},
		{
			Name:         "long-notes",
			Description:  "Keep every instruction",
			Instructions: instructions,
			Access:       sessionresource.MemoryAccessReadWrite,
			MountPath:    "/mnt/memory/long-notes",
		},
	})
	if strings.Contains(got, "下面列出的 rw 目录会跨会话保留") {
		t.Fatalf("MEMORY.md must not include the lifetime preamble yet:\n%s", got)
	}
	if !strings.HasPrefix(got, "<!-- oma-stores -->\n") {
		t.Fatalf("MEMORY.md must start with the store marker:\n%s", got)
	}
	markerIndex := strings.Index(got, "<!-- oma-stores -->")
	if markerIndex < 0 {
		t.Fatalf("missing store marker:\n%s", got)
	}
	storeBlock := got[markerIndex:]
	for _, line := range []string{
		"- [user-preferences](/mnt/memory/user-preferences) rw — 用户的饮食与语言偏好。问饮食或语言先读此目录；有新偏好就更新对应文件。",
		"- [oma-project](/mnt/memory/oma-project) rw — 架构决策。开始任务前先读；新结论写入 decisions/。",
		"- [team-playbook](/mnt/memory/team-playbook) ro — 团队规范，只读，不得修改。",
		"- [long-notes](/mnt/memory/long-notes) rw — Keep every instruction。" + instructions,
	} {
		if !strings.Contains(storeBlock, line) {
			t.Fatalf("missing store line %q in:\n%s", line, got)
		}
	}
	if !strings.Contains(got, instructions) {
		t.Fatal("500-rune instructions were truncated")
	}
	if utf8.RuneCountInString(instructions) != sessionresource.MaxMemoryInstructionsRunes {
		t.Fatalf("fixture instructions = %d runes", utf8.RuneCountInString(instructions))
	}
}

func TestRenderMemoryMarkdownFlattensNewlines(t *testing.T) {
	t.Parallel()
	storeLine := memoryMarkdownStoreLine(t, renderMemoryMarkdown([]memoryRuntimeMount{{
		Name:         "Team\nPlaybook",
		Description:  "Shared\r\nplaybook",
		Instructions: "Always\rcite sources.\u2028Never guess.",
		Access:       sessionresource.MemoryAccessReadWrite,
		MountPath:    "/mnt/memory/team-playbook",
	}}))
	want := "- [Team Playbook](/mnt/memory/team-playbook) rw — Shared playbook。Always cite sources. Never guess."
	if storeLine != want {
		t.Fatalf("store line = %q, want %q", storeLine, want)
	}
}

// memoryMarkdownStoreLine returns the single store line of a rendered
// MEMORY.md, failing when the render split one store across lines.
func memoryMarkdownStoreLine(t *testing.T, rendered string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "<!-- oma-stores -->" {
		t.Fatalf("rendered MEMORY.md is not a marker plus one store line:\n%s", rendered)
	}
	return lines[1]
}

func TestRenderMemoryMarkdownOmitsSeparatorsForEmptyFields(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		mount memoryRuntimeMount
		want  string
	}{
		{
			name: "description only",
			mount: memoryRuntimeMount{
				Name:        "user-preferences",
				Description: "用户的饮食与语言偏好",
				Access:      sessionresource.MemoryAccessReadWrite,
				MountPath:   "/mnt/memory/user-preferences",
			},
			want: "- [user-preferences](/mnt/memory/user-preferences) rw — 用户的饮食与语言偏好。",
		},
		{
			name: "instructions only",
			mount: memoryRuntimeMount{
				Name:         "team-playbook",
				Instructions: "开始任务前先读。",
				Access:       sessionresource.MemoryAccessReadOnly,
				MountPath:    "/mnt/memory/team-playbook",
			},
			want: "- [team-playbook](/mnt/memory/team-playbook) ro — 开始任务前先读。",
		},
		{
			name: "neither",
			mount: memoryRuntimeMount{
				Name:      "scratch",
				Access:    sessionresource.MemoryAccessReadWrite,
				MountPath: "/mnt/memory/scratch",
			},
			want: "- [scratch](/mnt/memory/scratch) rw",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := memoryMarkdownStoreLine(t, renderMemoryMarkdown([]memoryRuntimeMount{testCase.mount}))
			if got != testCase.want {
				t.Fatalf("store line = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRenderMemoryMarkdownEscapesLinkLabel(t *testing.T) {
	t.Parallel()
	got := memoryMarkdownStoreLine(t, renderMemoryMarkdown([]memoryRuntimeMount{{
		Name:      `notes](/etc/passwd) rw [x`,
		Access:    sessionresource.MemoryAccessReadOnly,
		MountPath: "/mnt/memory/notes",
	}}))
	// Every bracket from the name stays escaped, so a markdown reader cannot
	// close the label early and treat "/etc/passwd" as the mount path.
	want := `- [notes\](/etc/passwd) rw \[x](/mnt/memory/notes) ro`
	if got != want {
		t.Fatalf("store line = %q, want %q", got, want)
	}
}

func TestManagedAgentSessionConfigMemoryEnvironmentAndPrompt(t *testing.T) {
	t.Parallel()
	session := db.Session{
		AgentSnapshot: json.RawMessage(`{"model":{"id":"claude-opus-4-8"},"system":"You are a concise coding assistant."}`),
	}
	withoutStores := managedAgentSessionConfig(session, resolveManagedAgentRuntimeResources(nil))
	assertSessionConfigPromptExcludesMemory(t, withoutStores, "")
	assertSessionConfigMemoryEnv(t, withoutStores, false)

	withStores := managedAgentSessionConfig(session, resolveManagedAgentRuntimeResources([]db.SessionResource{
		memoryStoreSessionResource(
			"user-preferences",
			"user-preferences",
			sessionresource.MemoryAccessReadWrite,
			"用户的饮食与语言偏好",
			"问饮食或语言先读此目录",
		),
	}))
	assertSessionConfigPromptExcludesMemory(t, withStores, "user-preferences")
	assertSessionConfigMemoryEnv(t, withStores, true)

	payload, err := buildEnvironmentManagerV0Payload(
		"cse_test",
		"sk-ant-si-test-token",
		"sk-ant-oat01-test-token",
		1,
		"",
		withStores,
		config.Config{CodeSession: config.CodeSessionConfig{SandboxAPIBaseURL: "http://host.docker.internal:18081/"}},
		nil,
	)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	startup := body["startup_context"].(map[string]any)
	startupEnv := startup["environment_variables"].(map[string]any)
	if startupEnv["CLAUDE_CODE_REMOTE"] != "true" ||
		startupEnv["CLAUDE_CODE_REMOTE_MEMORY_DIR"] != sessionresource.MemoryMountRoot ||
		startupEnv["CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"] != sessionresource.MemoryMountRoot {
		t.Fatalf("startup memory env = %#v", startupEnv)
	}
	if sources, ok := startup["sources"].([]any); !ok || len(sources) != 0 {
		t.Fatalf("sources = %#v, want empty", startup["sources"])
	}
}

func memoryStoreSessionResource(name, slug, access, description, instructions string) db.SessionResource {
	payload, err := json.Marshal(map[string]any{
		"type":            sessionresource.MemoryStoreType,
		"memory_store_id": "mem_" + slug,
		"access":          access,
		"name":            name,
		"description":     description,
		"instructions":    instructions,
		"mount_path":      sessionresource.MemoryMountPath(slug),
	})
	if err != nil {
		panic(err)
	}
	return db.SessionResource{
		ResourceType: sessionresource.MemoryStoreType,
		Payload:      payload,
	}
}

func assertSessionConfigPromptExcludesMemory(t *testing.T, raw json.RawMessage, storeName string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode session config: %v", err)
	}
	prompt, _ := body["append_system_prompt"].(string)
	if prompt != managedAgentEnvironmentPrompt {
		t.Fatalf("append_system_prompt = %q", prompt)
	}
	if strings.Contains(prompt, sessionresource.MemoryMountRoot) ||
		strings.Contains(prompt, "MEMORY.md") ||
		(storeName != "" && strings.Contains(prompt, storeName)) {
		t.Fatalf("append_system_prompt leaked memory policy: %q", prompt)
	}
}

func assertSessionConfigMemoryEnv(t *testing.T, raw json.RawMessage, wantPresent bool) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode session config: %v", err)
	}
	env, _ := body["environment_variables"].(map[string]any)
	_, hasDir := env["CLAUDE_CODE_REMOTE_MEMORY_DIR"]
	_, hasOverride := env["CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"]
	if wantPresent {
		if env["CLAUDE_CODE_REMOTE_MEMORY_DIR"] != sessionresource.MemoryMountRoot ||
			env["CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"] != sessionresource.MemoryMountRoot {
			t.Fatalf("memory env = %#v", env)
		}
		return
	}
	if hasDir || hasOverride {
		t.Fatalf("memory env present without stores: %#v", env)
	}
}
