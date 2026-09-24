package tests

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSessionAgentWithOverrides(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-agent-overrides-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{
		"model":{"id":"claude-opus-4-6","speed":"fast"},
		"name":"session-overrides-agent",
		"system":"you are a researcher",
		"mcp_servers":[{"name":"linear","type":"url","url":"https://mcp.linear.app/mcp"}],
		"skills":[{"type":"anthropic","skill_id":"xlsx","version":"latest"}],
		"tools":[
			{"type":"agent_toolset_20260401","configs":[{"name":"read","enabled":true}]},
			{"type":"mcp_toolset","mcp_server_name":"linear"}
		]
	}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	updated := updateAgent(t, app, agent.ID, `{
		"version":1,
		"system":"you are version two"
	}`, http.StatusOK)
	if updated.Version != 2 {
		t.Fatalf("agent version = %d, want 2", updated.Version)
	}

	env := createEnvironment(t, app, `{"name":"session-overrides-env"}`)
	defer cleanupEnvironmentRows(t, app.pool, env.ID)

	t.Run("failure unknown type", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"deployment","id":`+quoteJSON(agent.ID)+`},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "agent.type must be agent or agent_with_overrides")
	})

	t.Run("failure override fields without type", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent","id":`+quoteJSON(agent.ID)+`,"system":null},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "agent override fields require type agent_with_overrides")
	})

	t.Run("failure null model", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"model":null},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "model cannot be null")
	})

	t.Run("failure unknown model", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"model":{"id":"not-configured"}},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, `model "not-configured" is not configured for this workspace`)
	})

	t.Run("failure clear tools while skills remain", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"tools":[]},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "skills require the read tool")
	})

	t.Run("failure disable read while skills remain", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"read","enabled":false}]}]},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "skills require the read tool")
	})

	t.Run("failure replace tools without read while skills remain", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"tools":[{"type":"mcp_toolset","mcp_server_name":"linear"}]},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "skills require the read tool")
	})

	t.Run("failure clear mcp servers while toolset remains", func(t *testing.T) {
		assertSessionCreateError(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(agent.ID)+`,"mcp_servers":[]},
			"environment_id":`+quoteJSON(env.ID)+`
		}`, "mcp_toolset.mcp_server_name must reference an MCP server")
	})

	t.Run("success clear system without rechecking inherited skills", func(t *testing.T) {
		bare := createSkillsOnlyAgent(t, app, "session-overrides-skills-without-tools")
		created := createSession(t, app, `{
			"agent":{"type":"agent_with_overrides","id":`+quoteJSON(bare.ID)+`,"system":null},
			"environment_id":`+quoteJSON(env.ID)+`
		}`)
		defer deleteSession(t, app, created.ID)
		assertRawContains(t, created.Agent, `"skill_id":"xlsx"`)
		snapshot := sessionAgentObject(t, created.Agent)
		if snapshot["system"] != nil {
			t.Fatalf("system = %#v, want nil", snapshot["system"])
		}
	})

	t.Run("success replace model and clear system on pinned version", func(t *testing.T) {
		created := createSession(t, app, `{
			"agent":{
				"type":"agent_with_overrides",
				"id":`+quoteJSON(agent.ID)+`,
				"version":1,
				"model":{"id":"claude-sonnet-4-6"},
				"system":null
			},
			"environment_id":`+quoteJSON(env.ID)+`
		}`)
		defer deleteSession(t, app, created.ID)

		snapshot := sessionAgentObject(t, created.Agent)
		if snapshot["id"] != agent.ID || snapshot["type"] != "agent" || snapshot["version"] != float64(1) {
			t.Fatalf("snapshot identity = %#v", snapshot)
		}
		assertRawContains(t, created.Agent, `"id":"claude-sonnet-4-6"`)
		assertRawContains(t, created.Agent, `"speed":"standard"`)
		if snapshot["system"] != nil {
			t.Fatalf("system = %#v, want nil", snapshot["system"])
		}
		assertRawContains(t, created.Agent, `"mcp_server_name":"linear"`)
		assertRawContains(t, created.Agent, `"skill_id":"xlsx"`)
		assertRawNotContains(t, created.Agent, `"you are a researcher"`)
		assertRawNotContains(t, created.Agent, `"you are version two"`)

		threads := listSessionThreads(t, app, created.ID, defaultTestKey)
		if len(threads.Data) != 1 {
			t.Fatalf("threads = %+v, want 1", threads.Data)
		}
		threadSnapshot := sessionAgentObject(t, threads.Data[0].Agent)
		if threadSnapshot["version"] != float64(1) || threadSnapshot["system"] != nil {
			t.Fatalf("thread snapshot = %#v", threadSnapshot)
		}
		assertRawContains(t, threads.Data[0].Agent, `"id":"claude-sonnet-4-6"`)

		retrieved := retrieveAgent(t, app, agent.ID, "")
		if retrieved.Version != 2 {
			t.Fatalf("agent version = %d, want 2", retrieved.Version)
		}
		assertRawContains(t, retrieved.Model, `"id":"claude-opus-4-6"`)
		assertRawContains(t, retrieved.Model, `"speed":"fast"`)
		if retrieved.System == nil || *retrieved.System != "you are version two" {
			t.Fatalf("agent system = %v", retrieved.System)
		}

		pinned := retrieveAgent(t, app, agent.ID, "version=1")
		if pinned.Version != 1 {
			t.Fatalf("pinned version = %d, want 1", pinned.Version)
		}
		assertRawContains(t, pinned.Model, `"id":"claude-opus-4-6"`)
		assertRawContains(t, pinned.Model, `"speed":"fast"`)
		if pinned.System == nil || *pinned.System != "you are a researcher" {
			t.Fatalf("pinned system = %v", pinned.System)
		}
		assertRawContains(t, pinned.Skills, `"skill_id":"xlsx"`)
		assertRawContains(t, pinned.Tools, `"name":"read"`)
		assertRawContains(t, pinned.MCPServers, `"name":"linear"`)
	})

	t.Run("success idle update replaces tools and mcp servers", func(t *testing.T) {
		created := createSession(t, app, `{
			"agent":{"type":"agent","id":`+quoteJSON(agent.ID)+`,"version":2},
			"environment_id":`+quoteJSON(env.ID)+`
		}`)
		defer deleteSession(t, app, created.ID)
		assertRawContains(t, created.Agent, `"version":2`)
		assertRawContains(t, created.Agent, `"you are version two"`)

		assertSessionUpdateError(t, app, created.ID, `{"agent":{"model":{"id":"claude-sonnet-4-6"}}}`, "session agent updates may only replace tools and mcp_servers")
		assertSessionUpdateError(t, app, created.ID, `{"agent":{"system":null}}`, "session agent updates may only replace tools and mcp_servers")
		assertSessionUpdateError(t, app, created.ID, `{"agent":{"skills":[]}}`, "session agent updates may only replace tools and mcp_servers")
		assertSessionUpdateError(t, app, created.ID, `{
			"agent":{"mcp_servers":[],"tools":[{"type":"mcp_toolset","mcp_server_name":"linear"}]}
		}`, "mcp_toolset.mcp_server_name must reference an MCP server")
		assertSessionUpdateError(t, app, created.ID, `{
			"agent":{"tools":[{"type":"mcp_toolset","mcp_server_name":"linear"}]}
		}`, "skills require the read tool")

		updatedSession := updateSession(t, app, created.ID, `{
			"agent":{
				"mcp_servers":[{"name":"github","type":"url","url":"https://mcp.github.example/mcp"}],
				"tools":[
					{"type":"agent_toolset_20260401","configs":[{"name":"read","enabled":true}]},
					{"type":"mcp_toolset","mcp_server_name":"github"}
				]
			}
		}`)
		assertRawContains(t, updatedSession.Agent, `"mcp_server_name":"github"`)
		assertRawContains(t, updatedSession.Agent, `"id":"claude-opus-4-6"`)
		assertRawContains(t, updatedSession.Agent, `"you are version two"`)
		assertRawNotContains(t, updatedSession.Agent, `"mcp_server_name":"linear"`)

		threads := listSessionThreads(t, app, created.ID, defaultTestKey)
		if len(threads.Data) != 1 || threads.Data[0].ParentThreadID != nil {
			t.Fatalf("primary thread = %+v", threads.Data)
		}
		assertRawContains(t, threads.Data[0].Agent, `"mcp_server_name":"github"`)
		assertRawNotContains(t, threads.Data[0].Agent, `"mcp_server_name":"linear"`)
	})

	t.Run("success idle update mcp servers without rechecking inherited tools", func(t *testing.T) {
		bare := createSkillsOnlyAgent(t, app, "session-overrides-idle-mcp-only")
		created := createSession(t, app, `{
			"agent":{"type":"agent","id":`+quoteJSON(bare.ID)+`},
			"environment_id":`+quoteJSON(env.ID)+`
		}`)
		defer deleteSession(t, app, created.ID)

		updatedSession := updateSession(t, app, created.ID, `{
			"agent":{"mcp_servers":[{"name":"github","type":"url","url":"https://mcp.github.example/mcp"}]}
		}`)
		assertRawContains(t, updatedSession.Agent, `"name":"github"`)
		assertRawContains(t, updatedSession.Agent, `"skill_id":"xlsx"`)
	})
}

func assertSessionCreateError(t *testing.T, app *testApp, body, wantMessage string) {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions?beta=true", strings.NewReader(body), defaultTestKey, true)
	assertSessionErrorMessage(t, resp, http.StatusBadRequest, wantMessage)
}

func assertSessionUpdateError(t *testing.T, app *testApp, sessionID, body, wantMessage string) {
	t.Helper()
	resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+sessionID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	assertSessionErrorMessage(t, resp, http.StatusBadRequest, wantMessage)
}

func assertSessionErrorMessage(t *testing.T, resp *http.Response, status int, wantMessage string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, status, readAll(t, resp.Body))
	}
	var body errorResponse
	decodeJSON(t, resp.Body, &body)
	if body.Type != "error" || body.Error.Type != "invalid_request_error" {
		t.Fatalf("error = %+v, want invalid_request_error", body)
	}
	if !strings.Contains(body.Error.Message, wantMessage) {
		t.Fatalf("error message = %q, want %q", body.Error.Message, wantMessage)
	}
}

func sessionAgentObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var object map[string]any
	decodeRawJSON(t, raw, &object)
	return object
}

func createSkillsOnlyAgent(t *testing.T, app *testApp, name string) agentAPIResponse {
	t.Helper()
	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":`+quoteJSON(name)+`,
		"skills":[{"type":"anthropic","skill_id":"xlsx","version":"latest"}]
	}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	return agent
}
