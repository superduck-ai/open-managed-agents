package dreams

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDreamContractRequiresBothBetas(t *testing.T) {
	handler := NewHandler(nil, nil)
	for _, test := range []struct {
		name  string
		betas string
		want  int
	}{
		{name: "missing dreaming beta", betas: managedAgentsBeta, want: http.StatusBadRequest},
		{name: "missing managed agents beta", betas: dreamingBeta, want: http.StatusBadRequest},
		{name: "both betas", betas: managedAgentsBeta + "," + dreamingBeta, want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("anthropic-version", defaultAnthropicAPIVersion)
			req.Header.Set("anthropic-beta", test.betas)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestCancelRouteExists(t *testing.T) {
	handler := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/drm_test/cancel", nil)
	req.Header.Set("anthropic-version", defaultAnthropicAPIVersion)
	req.Header.Set("anthropic-beta", managedAgentsBeta+","+dreamingBeta)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("POST /{id}/cancel status = %d, want %d (route must exist before auth)", response.Code, http.StatusUnauthorized)
	}
}

func TestCancelDreamStatusError(t *testing.T) {
	if err := cancelDreamStatusError("pending"); err != nil {
		t.Fatalf("pending cancel = %v, want nil", err)
	}
	if err := cancelDreamStatusError("canceled"); err != nil {
		t.Fatalf("canceled cancel = %v, want nil", err)
	}
	err := cancelDreamStatusError("completed")
	if err == nil {
		t.Fatal("completed cancel = nil, want error")
	}
	if err.Error() != "Dream has already ended" {
		t.Fatalf("completed cancel = %v", err)
	}
	if err := cancelDreamStatusError("failed"); err == nil {
		t.Fatal("failed cancel = nil, want error")
	}
}

func TestArchiveDreamStatusError(t *testing.T) {
	if err := archiveDreamStatusError("completed"); err != nil {
		t.Fatalf("completed archive = %v, want nil", err)
	}
	if err := archiveDreamStatusError("canceled"); err != nil {
		t.Fatalf("canceled archive = %v, want nil", err)
	}
	err := archiveDreamStatusError("pending")
	if err == nil {
		t.Fatal("pending archive = nil, want error")
	}
	if err.Error() != "cancel the Dream before archiving" {
		t.Fatalf("pending archive = %v", err)
	}
	if err := archiveDreamStatusError("running"); err == nil {
		t.Fatal("running archive = nil, want error")
	}
}

func TestResponseFromDreamExposesTerminalErrorAndUsage(t *testing.T) {
	endedAt := time.Date(2026, 9, 16, 10, 10, 2, 0, time.UTC)
	failed := responseFromDream(db.Dream{
		ExternalID: "drm_failed", Status: "failed", Model: "Qwen3.8-Max",
		Inputs:  json.RawMessage(`[{"type":"memory_store","memory_store_id":"memstore_in","session_ids":[]}]`),
		Outputs: json.RawMessage(`[]`),
		Error:   json.RawMessage(`{"type":"input_memory_store_unavailable","message":"input memory store memstore_in was archived after the Dream was created"}`),
		Usage:   json.RawMessage(`{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":5,"iterations":9}`),
		EndedAt: &endedAt,
	})
	if failed.Error == nil {
		t.Fatal("failed Dream error = nil, want the persisted error exposed")
	}
	if failed.Error.Type != "input_memory_store_unavailable" || failed.Error.Message == "" {
		t.Fatalf("failed Dream error = %#v", failed.Error)
	}
	if failed.EndedAt == nil || *failed.EndedAt != "2026-09-16T10:10:02Z" {
		t.Fatalf("failed Dream ended_at = %v", failed.EndedAt)
	}
	if failed.Usage != (dreamUsageResponse{InputTokens: 12, OutputTokens: 3, CacheReadInputTokens: 4, CacheCreationInputTokens: 5}) {
		t.Fatalf("failed Dream usage = %#v", failed.Usage)
	}

	for name, raw := range map[string]json.RawMessage{"nil": nil, "null": json.RawMessage(`null`), "legacy empty array": json.RawMessage(`[]`), "untyped": json.RawMessage(`{"message":"x"}`)} {
		pending := responseFromDream(db.Dream{ExternalID: "drm_pending", Status: "pending", Error: raw, Usage: json.RawMessage(`{}`)})
		if pending.Error != nil {
			t.Fatalf("%s: pending Dream error = %#v, want null", name, pending.Error)
		}
		if pending.EndedAt != nil || pending.Usage != (dreamUsageResponse{}) {
			t.Fatalf("%s: pending Dream = ended_at %v usage %#v, want empty", name, pending.EndedAt, pending.Usage)
		}
	}
	encoded, err := json.Marshal(responseFromDream(db.Dream{ExternalID: "drm_pending", Status: "pending"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"error":null`, `"ended_at":null`, `"usage":{"input_tokens":0,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("pending response %s does not contain %s", encoded, fragment)
		}
	}
}

func TestDreamCursorRoundTrip(t *testing.T) {
	want := db.Dream{UUID: "6d3ebf56-a71c-4987-85b5-1b7a5cab745f", CreatedAt: time.Date(2026, 9, 13, 12, 0, 0, 123, time.UTC)}
	cursor, err := decodeCursor(encodeCursor(want))
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if !cursor.CreatedAt.Equal(want.CreatedAt) || cursor.UUID != want.UUID {
		t.Fatalf("cursor = %#v, want %#v", cursor, want)
	}
}

func TestSessionIsDreamInternal(t *testing.T) {
	dreamTitle := "Dream drm_02au9Lu4wC1UmbH1OiR9kgRG"
	prepTitle := "Dream preparation drm_O1CEGbMz1xITrtqSt79YaQhv"
	userTitle := "记忆评测—08"
	namedDream := "Dream weekend plan"
	for _, test := range []struct {
		name    string
		session db.Session
		want    bool
	}{
		{
			name:    "metadata kind",
			session: db.Session{Metadata: json.RawMessage(`{"internal_kind":"dream","dream_id":"drm_test"}`)},
			want:    true,
		},
		{
			name:    "current internal title",
			session: db.Session{Title: &dreamTitle},
			want:    true,
		},
		{
			name:    "legacy preparation title",
			session: db.Session{Title: &prepTitle},
			want:    true,
		},
		{
			name:    "user review session",
			session: db.Session{Title: &userTitle, Metadata: json.RawMessage(`{"topic":"taste"}`)},
		},
		{
			name:    "unrelated dream title",
			session: db.Session{Title: &namedDream},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sessionIsDreamInternal(test.session); got != test.want {
				t.Fatalf("sessionIsDreamInternal() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestReviewSessionRejectsDreamInternal(t *testing.T) {
	title := "Dream drm_02au9Lu4wC1UmbH1OiR9kgRG"
	err := reviewSessionError(db.Session{ExternalID: "sesn_dream", Title: &title, Metadata: json.RawMessage(`{"internal_kind":"dream"}`)})
	if err == nil {
		t.Fatal("reviewSessionError() = nil, want Dream internal Session rejected")
	}
	if got := err.Error(); got != "sessions.session_ids must not include a Dream internal Session" {
		t.Fatalf("reviewSessionError() = %q", got)
	}
	if err := reviewSessionError(db.Session{ExternalID: "sesn_user"}); err != nil {
		t.Fatalf("reviewSessionError() = %v, want nil for a user Session", err)
	}
}

func TestConfiguredModel(t *testing.T) {
	if !configuredModel([]string{"claude-sonnet-4-6", "qwen3-coder-plus"}, "qwen3-coder-plus") {
		t.Fatal("configuredModel() = false, want configured model")
	}
	if configuredModel([]string{"claude-sonnet-4-6"}, "unknown-model") {
		t.Fatal("configuredModel() = true, want unconfigured model rejected")
	}
}

func TestDreamDefaultEnvironmentDefinition(t *testing.T) {
	if dreamDefaultEnvironmentName != "dream_env（Dream 内部环境）" || dreamDefaultEnvironmentKind != "dream_default_environment" {
		t.Fatalf("Dream default environment identity = (%q, %q)", dreamDefaultEnvironmentName, dreamDefaultEnvironmentKind)
	}
	var config struct {
		Type       string `json:"type"`
		Networking struct {
			Type string `json:"type"`
		} `json:"networking"`
		Packages struct {
			Type string `json:"type"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(dreamDefaultEnvironmentConfig, &config); err != nil {
		t.Fatalf("decode Dream default environment config: %v", err)
	}
	if config.Type != "cloud" || config.Networking.Type != "unrestricted" || config.Packages.Type != "packages" {
		t.Fatalf("Dream default environment config = %#v", config)
	}
}

func TestDreamDefaultAgentDefinition(t *testing.T) {
	if dreamDefaultAgentName != "dream_agent（Dream 内部 Agent）" || dreamDefaultAgentKind != "dream_default_agent" {
		t.Fatalf("Dream default agent identity = (%q, %q)", dreamDefaultAgentName, dreamDefaultAgentKind)
	}
	if dreamDefaultAgentSystem != nil {
		t.Fatalf("Dream default agent system = %q, want nil so instructions stay on the Dream row", *dreamDefaultAgentSystem)
	}
	var skills []struct {
		Type    string `json:"type"`
		SkillID string `json:"skill_id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(dreamDefaultAgentSkills, &skills); err != nil {
		t.Fatalf("decode Dream default agent skills: %v", err)
	}
	if len(skills) != 1 || skills[0].Type != "anthropic" || skills[0].SkillID != "dream" || skills[0].Version != "latest" {
		t.Fatalf("Dream default agent skills = %#v, want only the builtin dream skill", skills)
	}
	if string(dreamDefaultAgentMCPServers) != "[]" || string(dreamDefaultAgentTools) != "[]" {
		t.Fatalf("Dream default agent MCP/tools = (%s, %s), want empty arrays", dreamDefaultAgentMCPServers, dreamDefaultAgentTools)
	}
}

func TestFirstConfiguredModel(t *testing.T) {
	model, err := firstConfiguredModel([]string{" qwen3-coder-plus ", "claude-sonnet-4-6"})
	if err != nil || string(model) != `{"id":"qwen3-coder-plus"}` {
		t.Fatalf("firstConfiguredModel() = (%s, %v)", model, err)
	}
	if _, err := firstConfiguredModel([]string{"", "  "}); err == nil {
		t.Fatal("firstConfiguredModel() error = nil, want no configured model failure")
	}
}

func TestDreamSessionAgentSnapshotOverridesModelWithoutMutatingAgent(t *testing.T) {
	description := "Reusable system agent managed by Dream."
	agent := db.Agent{
		ExternalID:     "agent_dream_default",
		CurrentVersion: 1,
		Name:           dreamDefaultAgentName,
		Description:    &description,
		Model:          json.RawMessage(`{"id":"claude-sonnet-4-6"}`),
		MCPServers:     json.RawMessage(`[]`),
		Metadata:       json.RawMessage(`{"internal_kind":"dream_default_agent"}`),
		Multiagent:     json.RawMessage(`null`),
		Skills:         json.RawMessage(`[{"type":"anthropic","skill_id":"dream","version":"latest"}]`),
		Tools:          json.RawMessage(`[]`),
	}
	originalModel := append(json.RawMessage(nil), agent.Model...)

	snapshot, err := dreamSessionAgentSnapshot(agent, "qwen3-coder-plus")
	if err != nil {
		t.Fatalf("dreamSessionAgentSnapshot() error = %v", err)
	}

	var payload struct {
		Name   string          `json:"name"`
		System json.RawMessage `json:"system"`
		Model  struct {
			ID string `json:"id"`
		} `json:"model"`
		Skills []struct {
			SkillID string `json:"skill_id"`
		} `json:"skills"`
		MCPServers json.RawMessage `json:"mcp_servers"`
		Tools      json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(snapshot, &payload); err != nil {
		t.Fatalf("decode session snapshot: %v", err)
	}
	if payload.Name != dreamDefaultAgentName || payload.Model.ID != "qwen3-coder-plus" {
		t.Fatalf("session snapshot identity = (%q, %q), want shared agent with Dream model override", payload.Name, payload.Model.ID)
	}
	if len(payload.System) != 0 && string(payload.System) != "null" {
		t.Fatalf("session snapshot system = %s, want empty so instructions stay on the Dream row", payload.System)
	}
	if len(payload.Skills) != 1 || payload.Skills[0].SkillID != "dream" || string(payload.MCPServers) != "[]" || string(payload.Tools) != "[]" {
		t.Fatalf("session snapshot capabilities = skills=%#v mcp=%s tools=%s", payload.Skills, payload.MCPServers, payload.Tools)
	}
	if string(agent.Model) != string(originalModel) {
		t.Fatalf("shared agent model mutated to %s", agent.Model)
	}
}
