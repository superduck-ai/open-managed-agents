package tests

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentsnapshot"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestLegacySessionSubagentSnapshot(t *testing.T) {
	objects := newFakeStore("legacy-subagent-snapshot")
	app := newPayloadIntegrationApp(t, objects)
	seedBuiltinSkill(t, app, objects, "xlsx", "1")
	child := createAgent(t, app, `{"name":"计算Agent","model":"claude-opus-4-6","system":"child-v1","skills":[{"type":"anthropic","skill_id":"xlsx","version":"1"}],"mcp_servers":[{"name":"docs","type":"url","url":"https://docs.example/mcp"}],"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","permission_policy":{"type":"always_ask"}}]}]}`)
	parent := createAgent(t, app, `{"name":"parent","model":"claude-opus-4-6","system":"parent-v1","multiagent":{"type":"coordinator","agents":[{"type":"agent","id":`+quoteJSON(child.ID)+`,"version":1},{"type":"self"}]}}`)
	env := createEnvironment(t, app, `{"name":"legacy-subagent-snapshot"}`)
	created := createSession(t, app, `{"agent":`+quoteJSON(parent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	scope := getDefaultDBIDs(t, app.pool)
	session, found, err := app.db.GetSession(t.Context(), scope.WorkspaceUUID, created.ID)
	if err != nil || !found {
		t.Fatalf("load session: %v %t", err, found)
	}
	parentRecord, err := app.db.GetAgent(t.Context(), session.WorkspaceUUID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := agentsnapshot.FromAgent(parentRecord)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(legacy, &fields); err != nil {
		t.Fatal(err)
	}
	fields["future"] = json.RawMessage(`{"exact":9007199254740993}`)
	legacy, err = jsonv2.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := app.db.ReplaceSessionAgentSnapshot(t.Context(), session.WorkspaceUUID, session.UUID, session.AgentSnapshot, legacy)
	if err != nil || !changed {
		t.Fatalf("seed legacy snapshot: %v %t", err, changed)
	}
	session.AgentSnapshot = legacy

	updateAgent(t, app, child.ID, `{"version":1,"name":"updated child","model":"claude-sonnet-4-6","system":"child-v2","skills":[],"mcp_servers":[],"tools":[]}`, http.StatusOK)
	updateAgent(t, app, parent.ID, `{"version":1,"name":"updated parent","system":"parent-v2"}`, http.StatusOK)
	for _, test := range []struct{ name, roster string }{
		{"missing version", `{"agents":[{"id":` + quoteJSON(child.ID) + `,"type":"agent"}]}`},
		{"missing pinned version", `{"agents":[{"id":` + quoteJSON(child.ID) + `,"type":"agent","version":9999}]}`},
		{"unknown child", `{"agents":[{"id":"agent_missing","type":"agent","version":1}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalidFields := make(map[string]json.RawMessage)
			if err := jsonv2.Unmarshal(legacy, &invalidFields); err != nil {
				t.Fatal(err)
			}
			invalidFields["multiagent"] = json.RawMessage(test.roster)
			invalid, err := jsonv2.Marshal(invalidFields)
			if err != nil {
				t.Fatal(err)
			}
			next := session
			next.AgentSnapshot = invalid
			if _, err := agentsnapshot.ResolveSession(t.Context(), app.db, next); err == nil {
				t.Fatal("accepted invalid legacy reference")
			}
			stored, found, err := app.db.GetSession(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !found || !agentsnapshot.SameRawJSON(stored.AgentSnapshot, legacy) {
				t.Fatalf("failure changed stored snapshot: %v", err)
			}
		})
	}

	resolved, err := agentsnapshot.ResolveSession(t.Context(), app.db, session)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := agentsnapshot.ForSubagentType(resolved.AgentSnapshot, "计算Agent")
	if err != nil {
		t.Fatal(err)
	}
	var pinned struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		System  string `json:"system"`
		Model   struct {
			ID string `json:"id"`
		} `json:"model"`
		Skills     []json.RawMessage `json:"skills"`
		MCPServers []json.RawMessage `json:"mcp_servers"`
		Tools      []json.RawMessage `json:"tools"`
		Multiagent json.RawMessage   `json:"multiagent"`
	}
	if err := jsonv2.Unmarshal(raw, &pinned); err != nil {
		t.Fatal(err)
	}
	if pinned.ID != child.ID || pinned.Version != 1 || pinned.System != "child-v1" || pinned.Model.ID != "claude-opus-4-6" || len(pinned.Skills) != 1 || len(pinned.MCPServers) != 1 || len(pinned.Tools) != 1 {
		t.Fatalf("lost pinned child configuration: %s", raw)
	}
	self, err := agentsnapshot.ForSubagentType(resolved.AgentSnapshot, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if err := jsonv2.Unmarshal(self, &pinned); err != nil {
		t.Fatal(err)
	}
	if pinned.ID != parent.ID || pinned.Version != 1 || pinned.System != "parent-v1" || string(pinned.Multiagent) != "null" {
		t.Fatalf("lost pinned self: %s", self)
	}
	var resolvedFields map[string]json.RawMessage
	if err := jsonv2.Unmarshal(resolved.AgentSnapshot, &resolvedFields); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if key != "multiagent" && !agentsnapshot.SameRawJSON(value, resolvedFields[key]) {
			t.Fatalf("changed root field %s", key)
		}
	}
	stored, found, err := app.db.GetSession(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !found || !agentsnapshot.SameRawJSON(stored.AgentSnapshot, resolved.AgentSnapshot) {
		t.Fatalf("expanded snapshot not persisted: %v", err)
	}
	primary, _, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !agentsnapshot.SameRawJSON(primary.AgentSnapshot, resolved.AgentSnapshot) {
		t.Fatalf("primary snapshot not expanded: %v", err)
	}
	again, err := agentsnapshot.ResolveSession(t.Context(), nil, stored)
	if err != nil || !bytes.Equal(again.AgentSnapshot, stored.AgentSnapshot) {
		t.Fatalf("re-read resolved or changed complete snapshot: %v", err)
	}
	runtimeSkills, err := skills.NewRuntimeResolver(app.db).ResolveAgentSnapshot(t.Context(), session.WorkspaceUUID, stored.AgentSnapshot)
	if err != nil || len(runtimeSkills) != 1 || runtimeSkills[0].SkillID != "xlsx" {
		t.Fatalf("child skills not loaded: %+v %v", runtimeSkills, err)
	}

	t.Run("legacy deployment run", func(t *testing.T) {
		now := time.Now().UTC()
		deployment, err := app.deployments.Create(t.Context(), db.Deployment{
			UUID: uuid.NewV4().String(), ExternalID: "deploy_legacy",
			OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, CreatedByAPIKeyUUID: session.CreatedByAPIKeyUUID,
			EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
			AgentUUID: session.AgentUUID, AgentExternalID: session.AgentExternalID, AgentVersion: 1, AgentSnapshot: legacy,
			Name: "legacy", Metadata: json.RawMessage(`{}`), InitialEvents: json.RawMessage(`[]`), Resources: json.RawMessage(`[]`), ResourceSecrets: json.RawMessage(`{}`), VaultIDs: json.RawMessage(`[]`), Status: "active", CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := runDeployment(t, app, deployment.ExternalID)
		if run.SessionID == nil {
			t.Fatalf("deployment did not create session: %s", run.Error)
		}
		childSession, found, err := app.db.GetSession(t.Context(), session.WorkspaceUUID, *run.SessionID)
		if err != nil || !found || !agentsnapshot.SameRawJSON(childSession.AgentSnapshot, legacy) {
			t.Fatalf("deployment changed pinned reference: %v", err)
		}
		childSession, err = agentsnapshot.ResolveSession(t.Context(), app.db, childSession)
		if err != nil {
			t.Fatal(err)
		}
		if !agentsnapshot.SameRawJSON(childSession.AgentSnapshot, resolved.AgentSnapshot) {
			t.Fatal("deployment session expanded a different configuration")
		}
		primary, found, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, childSession.ExternalID)
		if err != nil || !found || !agentsnapshot.SameRawJSON(primary.AgentSnapshot, childSession.AgentSnapshot) {
			t.Fatalf("deployment primary snapshot not expanded: %v", err)
		}
	})

	codeID := launchLocalCodeSession(t, app, session.ExternalID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	postCodeSessionWorkerEvents(t, app, codeID, internalPayloadRequest(epoch,
		`{"type":"control_request","uuid":"dispatch","request_id":"dispatch-request","request":{"subtype":"can_use_tool","tool_name":"Agent","tool_use_id":"dispatch-tool","input":{"subagent_type":"计算Agent"}}}`,
		`{"type":"session.thread_created","uuid":"child-created","session_thread_id":"sthr_child","agent_name":"计算Agent","task_id":"child-runtime","tool_use_id":"dispatch-tool"}`,
		`{"type":"control_request","uuid":"child-permission","session_thread_id":"sthr_child","request_id":"child-request","request":{"subtype":"can_use_tool","tool_name":"Write","tool_use_id":"child-tool","input":{}}}`))
	tool := sessionEventObjectByType(t, listThreadEvents(t, app, session.ExternalID, "sthr_child", defaultTestKey), "agent.tool_use")
	if tool["evaluated_permission"] != "ask" {
		t.Fatalf("lost pinned child ask policy: %v", tool["evaluated_permission"])
	}
}

func TestSessionAgentSnapshotReplacementRejectsStaleAndForeignWrites(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("snapshot-replacement"))
	worker, _ := newPayloadIntegrationSession(t, app)
	session, found, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("load session: %v %t", err, found)
	}
	for _, test := range []struct {
		name, workspace string
		previous        json.RawMessage
	}{
		{"stale snapshot", session.WorkspaceUUID, json.RawMessage(`{"id":"stale"}`)},
		{"foreign workspace", "00000000-0000-0000-0000-000000000001", session.AgentSnapshot},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, err := app.db.ReplaceSessionAgentSnapshot(t.Context(), test.workspace, session.UUID, test.previous, json.RawMessage(`{"id":"replacement"}`))
			if err != nil || changed {
				t.Fatalf("stale or foreign replacement: %v %t", err, changed)
			}
			primary, _, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !agentsnapshot.SameRawJSON(primary.AgentSnapshot, session.AgentSnapshot) {
				t.Fatalf("changed primary snapshot: %v", err)
			}
		})
	}
	primary, _, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	child := db.SessionThread{UUID: "00000000-0000-0000-0000-000000000002", ExternalID: "sthr_existing", OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, SessionUUID: session.UUID, SessionExternalID: session.ExternalID, ParentThreadUUID: new(primary.UUID), AgentSnapshot: json.RawMessage(`{"id":"agent_existing"}`), Status: "idle", Usage: json.RawMessage(`{}`), Stats: json.RawMessage(`{}`)}
	createdChild, err := app.db.CreateSessionThreadIfAbsent(t.Context(), child)
	if err != nil {
		t.Fatal(err)
	}
	next := json.RawMessage(`{"id":"replacement"}`)
	changed, err := app.db.ReplaceSessionAgentSnapshot(t.Context(), session.WorkspaceUUID, session.UUID, session.AgentSnapshot, next)
	if err != nil || !changed {
		t.Fatalf("valid replacement failed: %v %t", err, changed)
	}
	storedChild, err := app.db.GetSessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID, createdChild.ExternalID)
	if err != nil || !agentsnapshot.SameRawJSON(storedChild.AgentSnapshot, createdChild.AgentSnapshot) {
		t.Fatalf("replacement changed child snapshot: %v", err)
	}
}
