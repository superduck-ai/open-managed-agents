package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type agentAPIResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	System      *string         `json:"system"`
	Model       json.RawMessage `json:"model"`
	MCPServers  json.RawMessage `json:"mcp_servers"`
	Metadata    json.RawMessage `json:"metadata"`
	Multiagent  json.RawMessage `json:"multiagent"`
	Skills      json.RawMessage `json:"skills"`
	Tools       json.RawMessage `json:"tools"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	Version     int             `json:"version"`
}

type agentPageResponse struct {
	Data     []agentAPIResponse `json:"data"`
	NextPage *string            `json:"next_page"`
}

func TestAgentsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("agents-bucket"))
	defer app.close()

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doAgentRequest(t, app, http.MethodGet, "/v1/agents?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doAgentRequest(t, app, http.MethodGet, "/v1/agents", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid created_at filter", func(t *testing.T) {
		resp := doAgentRequest(t, app, http.MethodGet, "/v1/agents?beta=true&created_at%5Bgte%5D=not-a-time", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure search missing beta query", func(t *testing.T) {
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents:search", strings.NewReader(`{"name":"agent"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure search malformed body", func(t *testing.T) {
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents:search?beta=true", strings.NewReader(`{`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid mcp tool reference", func(t *testing.T) {
		body := `{"model":"claude-opus-4-6","name":"bad mcp","tools":[{"type":"mcp_toolset","mcp_server_name":"missing"}]}`
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure unreferenced mcp server when using mcp toolsets", func(t *testing.T) {
		body := `{
			"model":"claude-opus-4-6",
			"name":"bad mcp roster",
			"mcp_servers":[
				{"name":"main","type":"url","url":"https://example.com/main"},
				{"name":"unused","type":"url","url":"https://example.com/unused"}
			],
			"tools":[{"type":"mcp_toolset","mcp_server_name":"main"}]
		}`
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid custom tool schema", func(t *testing.T) {
		body := `{
			"model":"claude-opus-4-6",
			"name":"bad custom tool",
			"tools":[{"type":"custom","name":"run_thing","description":"Runs a thing.","input_schema":{"properties":{"value":{"type":"string"}}}}]
		}`
		resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents?beta=true", strings.NewReader(body), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success create update archive list and versions", func(t *testing.T) {
		start := time.Now().UTC().Add(-time.Second)
		child := createAgent(t, app, `{"model":"claude-opus-4-6","name":"agents-test-child"}`)
		defer cleanupAgentRows(t, app.db, child.ID)
		assertRawContains(t, child.Model, `"speed":"standard"`)
		noopChild := updateAgent(t, app, child.ID, `{"version":1}`, http.StatusOK)
		if noopChild.Version != 1 {
			t.Fatalf("no-op update version = %d, want 1", noopChild.Version)
		}
		childVersions := listAgentVersions(t, app, child.ID, "limit=2")
		if len(childVersions.Data) != 1 || childVersions.Data[0].Version != 1 {
			t.Fatalf("no-op update created a new version: %+v", childVersions)
		}

		defaultTools := createAgent(t, app, `{
			"model":"claude-opus-4-6",
			"name":"agents-test-default-tools",
			"mcp_servers":[{"name":"main","type":"url","url":"https://example.com/sse"}],
			"tools":[
				{"type":"agent_toolset_20260401","configs":[{"name":"web_fetch","enabled":false}]},
				{"type":"mcp_toolset","mcp_server_name":"main","configs":[{"name":"remote","enabled":false}]}
			]
		}`)
		defer cleanupAgentRows(t, app.db, defaultTools.ID)
		var toolConfigs []map[string]any
		decodeRawJSON(t, defaultTools.Tools, &toolConfigs)
		if got := toolConfigs[0]["default_config"].(map[string]any)["permission_policy"].(map[string]any)["type"]; got != "always_allow" {
			t.Fatalf("agent toolset default permission = %v, want always_allow", got)
		}
		if got := toolConfigs[1]["default_config"].(map[string]any)["permission_policy"].(map[string]any)["type"]; got != "always_ask" {
			t.Fatalf("mcp toolset default permission = %v, want always_ask", got)
		}

		sibling := createAgent(t, app, `{"model":"claude-opus-4-6","name":"agents-test-sibling"}`)
		defer cleanupAgentRows(t, app.db, sibling.ID)

		parentBody := `{
			"model":{"id":"claude-opus-4-6","speed":"standard"},
			"name":"agents-test-parent",
			"description":"first description",
			"system":"first system",
			"mcp_servers":[{"name":"main","type":"url","url":"https://example.com/sse"}],
			"metadata":{"a":"b"},
			"multiagent":{"type":"coordinator","agents":[` + quoteJSON(child.ID) + `,{"type":"self"}]},
			"skills":[{"type":"anthropic","skill_id":"xlsx","version":"1"}],
			"tools":[
				{"type":"agent_toolset_20260401","configs":[{"name":"bash","enabled":true,"permission_policy":{"type":"always_allow"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}},
				{"type":"mcp_toolset","mcp_server_name":"main","configs":[{"name":"remote","enabled":true,"permission_policy":{"type":"always_ask"}}],"default_config":{"enabled":true,"permission_policy":{"type":"always_allow"}}},
				{"type":"custom","name":"custom_tool","description":"A custom test tool.","input_schema":{"type":"object","properties":{"value":{"type":"string"}}}}
			]
		}`
		parent := createAgent(t, app, parentBody)
		defer cleanupAgentRows(t, app.db, parent.ID)
		if parent.Type != "agent" || parent.Version != 1 || parent.Name != "agents-test-parent" {
			t.Fatalf("unexpected created parent: %+v", parent)
		}
		assertRawContains(t, parent.Model, `"speed":"standard"`)
		assertRawContains(t, parent.Multiagent, `"id":"`+child.ID+`"`)
		assertRawContains(t, parent.Multiagent, `"id":"`+parent.ID+`"`)

		updateBody := `{
			"version":1,
			"name":"agents-test-parent-v2",
			"description":null,
			"system":null,
			"metadata":{"a":"","c":"d"},
			"model":"claude-sonnet-4-6",
			"mcp_servers":[],
			"skills":[],
			"tools":[],
			"multiagent":null
		}`
		updated := updateAgent(t, app, parent.ID, updateBody, http.StatusOK)
		if updated.Version != 2 || updated.Name != "agents-test-parent-v2" || updated.Description != nil || updated.System != nil {
			t.Fatalf("unexpected updated parent: %+v", updated)
		}
		assertRawContains(t, updated.Model, `"id":"claude-sonnet-4-6"`)
		assertRawContains(t, updated.Metadata, `"c":"d"`)
		assertRawNotContains(t, updated.Metadata, `"a"`)
		if string(updated.Multiagent) != "null" {
			t.Fatalf("updated multiagent = %s, want null", updated.Multiagent)
		}

		conflictResp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+parent.ID+"?beta=true", strings.NewReader(`{"version":1,"name":"stale"}`), defaultTestKey, true)
		assertError(t, conflictResp, http.StatusConflict, "conflict_error")

		versionOne := retrieveAgent(t, app, parent.ID, "version=1")
		if versionOne.Version != 1 || versionOne.Name != "agents-test-parent" || versionOne.Description == nil {
			t.Fatalf("unexpected version one snapshot: %+v", versionOne)
		}
		latest := retrieveAgent(t, app, parent.ID, "")
		if latest.Version != 2 || latest.Name != "agents-test-parent-v2" {
			t.Fatalf("unexpected latest agent: %+v", latest)
		}

		versionsPage1 := listAgentVersions(t, app, parent.ID, "limit=1")
		if len(versionsPage1.Data) != 1 || versionsPage1.Data[0].Version != 2 || versionsPage1.NextPage == nil {
			t.Fatalf("unexpected first versions page: %+v", versionsPage1)
		}
		versionsPage2 := listAgentVersions(t, app, parent.ID, "limit=1&page="+url.QueryEscape(*versionsPage1.NextPage))
		if len(versionsPage2.Data) != 1 || versionsPage2.Data[0].Version != 1 {
			t.Fatalf("unexpected second versions page: %+v", versionsPage2)
		}

		archived := archiveAgent(t, app, parent.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archive archived_at = nil, want timestamp")
		}
		archivedAgain := archiveAgent(t, app, parent.ID)
		if archivedAgain.ArchivedAt == nil {
			t.Fatalf("idempotent archive archived_at = nil")
		}
		updateArchivedResp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+parent.ID+"?beta=true", strings.NewReader(`{"version":2,"name":"nope"}`), defaultTestKey, true)
		assertError(t, updateArchivedResp, http.StatusBadRequest, "invalid_request_error")

		filter := "created_at%5Bgte%5D=" + url.QueryEscape(start.Format(time.RFC3339Nano))
		defaultPage := listAgents(t, app, filter)
		if containsAgent(defaultPage.Data, parent.ID) {
			t.Fatalf("default list included archived parent: %+v", defaultPage.Data)
		}
		if !containsAgent(defaultPage.Data, child.ID) || !containsAgent(defaultPage.Data, sibling.ID) {
			t.Fatalf("default list missing active agents: %+v", defaultPage.Data)
		}
		archivedPage := listAgents(t, app, filter+"&include_archived=true")
		if !containsAgent(archivedPage.Data, parent.ID) {
			t.Fatalf("include_archived list missing parent: %+v", archivedPage.Data)
		}
		page1 := listAgents(t, app, filter+"&limit=1")
		if len(page1.Data) != 1 || page1.NextPage == nil {
			t.Fatalf("unexpected agents first page: %+v", page1)
		}
		page2 := listAgents(t, app, filter+"&limit=1&page="+url.QueryEscape(*page1.NextPage))
		if len(page2.Data) != 1 {
			t.Fatalf("unexpected agents second page: %+v", page2)
		}
	})

	t.Run("success search agents by name with archived filtering pagination and workspace isolation", func(t *testing.T) {
		suffix := strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
		active := createAgent(t, app, `{"model":"claude-opus-4-6","name":"Search Alpha `+suffix+`"}`)
		defer cleanupAgentRows(t, app.db, active.ID)
		archived := createAgent(t, app, `{"model":"claude-opus-4-6","name":"search alpha `+suffix+` archived"}`)
		defer cleanupAgentRows(t, app.db, archived.ID)
		otherName := createAgent(t, app, `{"model":"claude-opus-4-6","name":"Search Beta `+suffix+`"}`)
		defer cleanupAgentRows(t, app.db, otherName.ID)
		archiveAgent(t, app, archived.ID)

		otherKey := "sk-ant-search-other-" + suffix
		seedWorkspaceKey(t, app.db, "org_agents_search_other_"+suffix, "workspace_agents_search_other_"+suffix, "api_key_agents_search_other_"+suffix, otherKey)
		otherWorkspaceAgent := createAgentWithKey(t, app, `{"model":"claude-opus-4-6","name":"Search Alpha `+suffix+`"}`, otherKey)
		defer cleanupAgentRows(t, app.db, otherWorkspaceAgent.ID)

		activePage := searchAgents(t, app, `{"name":"alpha `+suffix+`","limit":10}`)
		if !containsAgent(activePage.Data, active.ID) {
			t.Fatalf("search missing active agent: %+v", activePage.Data)
		}
		if containsAgent(activePage.Data, archived.ID) || containsAgent(activePage.Data, otherName.ID) || containsAgent(activePage.Data, otherWorkspaceAgent.ID) {
			t.Fatalf("search returned unexpected agent: %+v", activePage.Data)
		}

		archivedPage := searchAgents(t, app, `{"name":"alpha `+suffix+`","limit":10,"include_archived":true}`)
		if !containsAgent(archivedPage.Data, archived.ID) {
			t.Fatalf("include_archived search missing archived agent: %+v", archivedPage.Data)
		}

		page1 := searchAgents(t, app, `{"name":"search","limit":1,"include_archived":true}`)
		if len(page1.Data) != 1 || page1.NextPage == nil {
			t.Fatalf("unexpected search first page: %+v", page1)
		}
		page2 := searchAgents(t, app, `{"name":"search","limit":1,"include_archived":true,"page":`+quoteJSON(*page1.NextPage)+`}`)
		if len(page2.Data) != 1 {
			t.Fatalf("unexpected search second page: %+v", page2)
		}

		otherWorkspacePage := searchAgentsWithKey(t, app, `{"name":"alpha `+suffix+`","limit":10}`, otherKey)
		if !containsAgent(otherWorkspacePage.Data, otherWorkspaceAgent.ID) || containsAgent(otherWorkspacePage.Data, active.ID) {
			t.Fatalf("workspace-isolated search returned wrong agents: %+v", otherWorkspacePage.Data)
		}
	})
}

func TestAgentsSchemaHasNoForeignKeys(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("agents-schema-bucket"))
	defer app.close()

	var foreignKeyCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
			and cls.relname in ('agents', 'agent_versions')
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count agents foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("agents foreign key count = %d, want 0", foreignKeyCount)
	}
}

func TestAgentsOfficialSDKFixture(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("agents-fixture-bucket"))
	defer app.close()

	updateResp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+app.cfg.SDKFixtures.AgentID+"?beta=true", strings.NewReader(`{"version":1,"name":"fixture"}`), config.OfficialSDKResourceAPIKey, true)
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("fixture update status = %d, want 200: %s", updateResp.StatusCode, readAll(t, updateResp.Body))
	}
	var updated agentAPIResponse
	decodeJSON(t, updateResp.Body, &updated)
	if updated.ID != app.cfg.SDKFixtures.AgentID || updated.Version != 2 {
		t.Fatalf("unexpected fixture update response: %+v", updated)
	}

	archiveResp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+app.cfg.SDKFixtures.AgentID+"/archive?beta=true", nil, config.OfficialSDKResourceAPIKey, true)
	defer archiveResp.Body.Close()
	if archiveResp.StatusCode != http.StatusOK {
		t.Fatalf("fixture archive status = %d, want 200: %s", archiveResp.StatusCode, readAll(t, archiveResp.Body))
	}
	var archived agentAPIResponse
	decodeJSON(t, archiveResp.Body, &archived)
	if archived.ArchivedAt == nil {
		t.Fatalf("fixture archived_at = nil")
	}
}

func doAgentRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new agent request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do agent request: %v", err)
	}
	return resp
}

func createAgent(t *testing.T, app *testApp, body string) agentAPIResponse {
	t.Helper()
	return createAgentWithKey(t, app, body, defaultTestKey)
}

func createAgentWithKey(t *testing.T, app *testApp, body string, key string) agentAPIResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents?beta=true", strings.NewReader(body), key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create agent status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var agent agentAPIResponse
	decodeJSON(t, resp.Body, &agent)
	if agent.ID == "" {
		t.Fatalf("create agent returned empty id: %+v", agent)
	}
	return agent
}

func searchAgents(t *testing.T, app *testApp, body string) agentPageResponse {
	t.Helper()
	return searchAgentsWithKey(t, app, body, defaultTestKey)
}

func searchAgentsWithKey(t *testing.T, app *testApp, body string, key string) agentPageResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents:search?beta=true", strings.NewReader(body), key, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search agents status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page agentPageResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func updateAgent(t *testing.T, app *testApp, agentID, body string, wantStatus int) agentAPIResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+agentID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("update agent status = %d, want %d: %s", resp.StatusCode, wantStatus, readAll(t, resp.Body))
	}
	var agent agentAPIResponse
	if wantStatus == http.StatusOK {
		decodeJSON(t, resp.Body, &agent)
	}
	return agent
}

func retrieveAgent(t *testing.T, app *testApp, agentID, query string) agentAPIResponse {
	t.Helper()
	path := "/v1/agents/" + agentID + "?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doAgentRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve agent status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var agent agentAPIResponse
	decodeJSON(t, resp.Body, &agent)
	return agent
}

func archiveAgent(t *testing.T, app *testApp, agentID string) agentAPIResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodPost, "/v1/agents/"+agentID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive agent status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var agent agentAPIResponse
	decodeJSON(t, resp.Body, &agent)
	return agent
}

func listAgents(t *testing.T, app *testApp, query string) agentPageResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodGet, "/v1/agents?beta=true&"+query, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list agents status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page agentPageResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func listAgentVersions(t *testing.T, app *testApp, agentID, query string) agentPageResponse {
	t.Helper()
	resp := doAgentRequest(t, app, http.MethodGet, "/v1/agents/"+agentID+"/versions?beta=true&"+query, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list agent versions status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page agentPageResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func containsAgent(agents []agentAPIResponse, id string) bool {
	for _, agent := range agents {
		if agent.ID == id {
			return true
		}
	}
	return false
}

func cleanupAgentRows(t *testing.T, database *db.DB, agentID string) {
	t.Helper()
	if _, err := database.Pool.Exec(context.Background(), `delete from agent_versions where agent_external_id = $1`, agentID); err != nil {
		t.Fatalf("cleanup agent versions: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `delete from agents where external_id = $1`, agentID); err != nil {
		t.Fatalf("cleanup agent: %v", err)
	}
}

func quoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func assertRawContains(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	if !strings.Contains(string(raw), want) {
		t.Fatalf("raw JSON %s does not contain %s", raw, want)
	}
}

func assertRawNotContains(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	if strings.Contains(string(raw), want) {
		t.Fatalf("raw JSON %s unexpectedly contains %s", raw, want)
	}
}

func decodeRawJSON(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode raw JSON %s: %v", raw, err)
	}
}
