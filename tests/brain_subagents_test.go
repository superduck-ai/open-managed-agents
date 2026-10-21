//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBrainSubagentAssignmentRejectsInvalidLookups(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-subagents-invalid"))
	code, _ := newPayloadIntegrationSession(t, app)
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	session, found, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("load session: %v", err)
	}
	session.AgentSnapshot = json.RawMessage(`{"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"agent_missing","version":3}]}}`)
	if _, _, err := app.db.UpdateSession(t.Context(), code.WorkspaceUUID, session.ExternalID, session); err != nil {
		t.Fatal(err)
	}
	if _, err := createRuntimeTestSession(t, app, service, code, session, json.RawMessage(`{}`)); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing child version accepted: %v", err)
	}
}

func TestBrainSubagentAssignmentPinsDatabaseVersion(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("brain-subagents-version"))
	code, _ := newPayloadIntegrationSession(t, app)
	prompt, description := "Pinned instructions", "Read independently"
	child, err := app.db.CreateAgent(t.Context(), db.Agent{
		UUID: uuid.NewString(), ExternalID: "agent_brain_child", WorkspaceUUID: code.WorkspaceUUID,
		Name: "reader", System: &prompt, Description: &description, Model: json.RawMessage(`{"id":"child-model"}`),
		MCPServers: json.RawMessage(`[]`), Skills: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`),
		Tools:      json.RawMessage(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`),
		Multiagent: json.RawMessage(`{"type":"coordinator","agents":[{"type":"agent","id":"unreachable","version":1}]}`),
		CreatedAt:  time.Now().UTC(),
	}, "agv_brain_child_1")
	if err != nil {
		t.Fatal(err)
	}
	child.Name = "changed"
	if _, err := app.db.UpdateAgent(t.Context(), code.WorkspaceUUID, child.ExternalID, 1, child, "agv_brain_child_2"); err != nil {
		t.Fatal(err)
	}
	session, found, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("load session: %v", err)
	}
	session.AgentSnapshot = json.RawMessage(`{"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"agent_brain_child","version":1}]}}`)
	if _, _, err := app.db.UpdateSession(t.Context(), code.WorkspaceUUID, session.ExternalID, session); err != nil {
		t.Fatal(err)
	}
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	created, err := createRuntimeTestSession(t, app, service, code, session, json.RawMessage(`{"system":"parent","future":{"n":9007199254740993},"skills":[{"name":"skill"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, found, err := app.db.GetCodeSession(t.Context(), created.CodeSessionID)
	if err != nil || !found {
		t.Fatalf("load assignment: %v", err)
	}
	var metadata struct {
		Config struct {
			System string `json:"system"`
			Future struct {
				N json.Number `json:"n"`
			} `json:"future"`
			Skills []struct {
				Name string `json:"name"`
			} `json:"skills"`
			Subagents codesessions.RuntimeSubagentConfig `json:"subagents"`
		} `json:"config"`
	}
	if err := json.Unmarshal(stored.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	launch := metadata.Config
	expected := codesessions.RuntimeSubagentConfig{InlineAgents: false, Agents: []codesessions.RuntimeSubagentDefinition{
		{Name: "reader", Description: description, SystemPrompt: prompt, Model: "child-model", Tools: []string{"Read"}},
	}}
	var public struct {
		Type       string          `json:"type"`
		ID         string          `json:"id"`
		Version    int             `json:"version"`
		Name       string          `json:"name"`
		System     string          `json:"system"`
		Tools      json.RawMessage `json:"tools"`
		Multiagent json.RawMessage `json:"multiagent"`
	}
	if len(launch.Subagents.Agents) != 1 {
		t.Fatalf("missing pinned agent: %+v", launch.Subagents)
	}
	if err := json.Unmarshal(launch.Subagents.Agents[0].AgentSnapshot, &public); err != nil {
		t.Fatal(err)
	}
	if public.Type != "agent" || public.ID != "agent_brain_child" || public.Version != 1 || public.Name != "reader" || public.System != prompt || len(public.Multiagent) != 0 {
		t.Fatalf("public snapshot not pinned: %s", launch.Subagents.Agents[0].AgentSnapshot)
	}
	var actualTools, expectedTools any
	if err := json.Unmarshal(public.Tools, &actualTools); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`), &expectedTools); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualTools, expectedTools) {
		t.Fatalf("permission policy lost: %s", public.Tools)
	}

	expected.Agents[0].AgentSnapshot = launch.Subagents.Agents[0].AgentSnapshot
	if !reflect.DeepEqual(launch.Subagents, expected) || launch.System != "parent" || launch.Future.N.String() != "9007199254740993" || len(launch.Skills) != 1 || launch.Skills[0].Name != "skill" {
		t.Fatalf("pinned launch changed: %+v", launch)
	}
}

func createRuntimeTestSession(t *testing.T, app *testApp, service *codesessions.Service, code db.CodeSession, session db.Session, config json.RawMessage) (codesessions.ManagedAgentCreateResult, error) {
	t.Helper()
	env, err := app.db.GetEnvironmentByUUID(t.Context(), code.WorkspaceUUID, code.EnvironmentUUID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := app.db.GetLatestEnvironmentWorkForSession(t.Context(), code.WorkspaceUUID, code.EnvironmentExternalID, code.SessionUUID)
	if err != nil {
		t.Fatal(err)
	}
	return service.CreateManagedAgentCodeSession(t.Context(), codesessions.ManagedAgentCreateInput{Session: session, Environment: env, EnvironmentWork: work, Model: code.Model, WorkDir: code.WorkDir, Config: config})
}
