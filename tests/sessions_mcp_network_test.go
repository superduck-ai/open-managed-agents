package tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestSessionCreationValidatesDeclaredMCPHosts(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("session-mcp-network"))
	defer app.close()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"network-check","mcp_servers":[{"name":"peer","type":"url","url":"https://api.example.com/mcp"},{"name":"second","type":"url","url":"https://other.example.net/mcp"}],"tools":[{"type":"mcp_toolset","mcp_server_name":"peer"},{"type":"mcp_toolset","mcp_server_name":"second"}]}`)
	defer cleanupAgentRows(t, app.pool, agent.ID)
	for _, tc := range []struct {
		name, networking string
		status           int
	}{
		{"empty allowlist", `{"type":"limited","allowed_hosts":[]}`, 400},
		{"one blocked server", `{"type":"limited","allowed_hosts":["api.example.com"]}`, 400},
		{"explicit false", `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false}`, 400},
		{"package exception is not MCP exception", `{"type":"limited","allowed_hosts":[],"allow_package_managers":true}`, 400},
		{"all hosts allowed", `{"type":"limited","allowed_hosts":["*.example.com","other.example.net"]}`, 200},
		{"MCP exception", `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}`, 200},
		{"unrestricted", `{"type":"unrestricted"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := createEnvironment(t, app, `{"name":"network-check","config":{"type":"cloud","networking":`+tc.networking+`}}`)
			defer cleanupEnvironmentRows(t, app.pool, env.ID)
			resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions", strings.NewReader(`{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`), defaultTestKey, true)
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var session sessionAPIResponse
				decodeJSON(t, resp.Body, &session)
				if session.ID == "" {
					t.Fatal("missing Session id")
				}
				defer cleanupSession(t, app, session.ID)
			}
			if resp.StatusCode != tc.status {
				t.Fatalf("status=%d want=%d: %s", resp.StatusCode, tc.status, readAll(t, resp.Body))
			}
			if tc.status != 200 {
				var count int
				if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE environment_uuid=(SELECT uuid FROM environments WHERE external_id=$1)`, env.ID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatalf("rejection persisted %d Sessions", count)
				}
			}
		})
	}
}
