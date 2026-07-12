package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/mcpcatalogs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	testMCPAvailable int32 = iota
	testMCPUnavailable
)

type controllableMCPServer struct {
	server *httptest.Server
	mode   atomic.Int32
}

type testCatalogItem struct {
	ServerName string                  `json:"server_name"`
	Status     string                  `json:"status"`
	Tools      []db.MCPToolCatalogItem `json:"tools"`
}

type testCatalogListResponse struct {
	Data    []testCatalogItem `json:"data"`
	Version int               `json:"version"`
}

type testCatalogRefreshResponse struct {
	Data    testCatalogItem `json:"data"`
	Version int             `json:"version"`
}

func TestMCPToolCatalogConsoleAPI(t *testing.T) {
	weather := newControllableMCPServer(t, &mcp.Tool{
		Name:        "get_forecast",
		Title:       "Get forecast",
		Description: "Returns a weather forecast.",
	})
	empty := newControllableMCPServer(t)
	weatherEndpoint := weather.server.URL + "/mcp?toolsets=all"

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("mcp-catalog-console-bucket"))
	defer app.close()

	agent := createAgent(t, app, fmt.Sprintf(`{
		"model":"claude-opus-4-6",
		"name":"mcp-catalog-console-agent",
		"mcp_servers":[
			{"name":"weather","type":"url","url":%q},
			{"name":"empty","type":"url","url":%q}
		]
		}`, weatherEndpoint, empty.server.URL))
	defer cleanupAgentRows(t, app.db, agent.ID)
	defer deleteTestMCPCatalogByEndpoint(t, app.db, weatherEndpoint)
	defer deleteTestMCPCatalogByEndpoint(t, app.db, empty.server.URL)

	cookies := app.platformLoginCookies(t, "mcp-catalog-console@example.com")
	orgCookie := responseCookie(cookies, "lastActiveOrg")
	if orgCookie == nil {
		t.Fatal("platform login did not return lastActiveOrg")
	}
	basePath := "/api/console/organizations/" + orgCookie.Value + "/workspaces/"
	agentPath := basePath + "default/agents/" + agent.ID + "/mcp_tool_catalogs"

	// 失败和边界场景先执行，确保请求校验或探测失败不会创建成功快照。
	t.Run("rejects a workspace outside the authenticated Agent scope", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, basePath+"other/agents/"+agent.ID+"/mcp_tool_catalogs", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-workspace status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("rejects a client supplied endpoint", func(t *testing.T) {
		body := strings.NewReader(`{"server_name":"weather","url":"https://attacker.example/mcp"}`)
		resp := app.platformRequest(t, http.MethodPost, agentPath+"/refresh?version=1", body, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("arbitrary refresh URL status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("rejects an unknown server name", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodPost, agentPath+"/refresh?version=1", strings.NewReader(`{"server_name":"missing"}`), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("unknown server status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("GET missing is unknown and does not create a row", func(t *testing.T) {
		payload := getTestMCPCatalogs(t, app, agentPath+"?version=1", cookies)
		if payload.Version != 1 || len(payload.Data) != 2 {
			t.Fatalf("catalog response = %#v", payload)
		}
		weatherItem := findTestCatalogItem(t, payload.Data, "weather")
		if weatherItem.Status != "unknown" || weatherItem.Tools != nil {
			t.Fatalf("missing weather catalog = %#v, want unknown/null", weatherItem)
		}
		if _, err := app.db.GetMCPToolCatalog(context.Background(), "url", weatherEndpoint); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("catalog after GET error = %v, want ErrNotFound", err)
		}
	})

	t.Run("upstream failure before first success leaves no row", func(t *testing.T) {
		weather.mode.Store(testMCPUnavailable)
		resp := refreshTestMCPCatalog(t, app, agentPath, "weather", cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("upstream failure status = %d, want 502: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		if _, err := app.db.GetMCPToolCatalog(context.Background(), "url", weatherEndpoint); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("catalog after failed refresh error = %v, want ErrNotFound", err)
		}
	})

	t.Run("successful refresh saves and returns typed tools", func(t *testing.T) {
		weather.mode.Store(testMCPAvailable)
		resp := refreshTestMCPCatalog(t, app, agentPath, "weather", cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("refresh status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var payload testCatalogRefreshResponse
		decodeJSON(t, resp.Body, &payload)
		if payload.Version != 1 || payload.Data.ServerName != "weather" || payload.Data.Status != "ready" || len(payload.Data.Tools) != 1 {
			t.Fatalf("refresh response = %#v", payload)
		}
		tool := payload.Data.Tools[0]
		if tool.Name != "get_forecast" || tool.Title != "Get forecast" || tool.Description != "Returns a weather forecast." {
			t.Fatalf("refresh tool = %#v", tool)
		}

		listed := getTestMCPCatalogs(t, app, agentPath+"?version=1", cookies)
		weatherItem := findTestCatalogItem(t, listed.Data, "weather")
		if weatherItem.Status != "ready" || len(weatherItem.Tools) != 1 || weatherItem.Tools[0].Name != "get_forecast" {
			t.Fatalf("saved weather catalog = %#v", weatherItem)
		}
	})

	t.Run("failure after success preserves the last good snapshot", func(t *testing.T) {
		before, err := app.db.GetMCPToolCatalog(context.Background(), "url", weatherEndpoint)
		if err != nil {
			t.Fatalf("load catalog before failed refresh: %v", err)
		}
		weather.mode.Store(testMCPUnavailable)
		resp := refreshTestMCPCatalog(t, app, agentPath, "weather", cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("failed refresh status = %d, want 502: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		after, err := app.db.GetMCPToolCatalog(context.Background(), "url", weatherEndpoint)
		if err != nil {
			t.Fatalf("load catalog after failed refresh: %v", err)
		}
		if !after.UpdatedAt.Equal(before.UpdatedAt) || len(after.Tools) != 1 || after.Tools[0].Name != "get_forecast" {
			t.Fatalf("catalog changed after failed refresh: before=%#v after=%#v", before, after)
		}
	})

	t.Run("successful empty list remains a known empty array", func(t *testing.T) {
		resp := refreshTestMCPCatalog(t, app, agentPath, "empty", cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("empty refresh status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var payload testCatalogRefreshResponse
		decodeJSON(t, resp.Body, &payload)
		if payload.Data.Status != "ready" || payload.Data.Tools == nil || len(payload.Data.Tools) != 0 {
			t.Fatalf("empty refresh response = %#v, want ready/[]", payload.Data)
		}
		listed := getTestMCPCatalogs(t, app, agentPath, cookies)
		emptyItem := findTestCatalogItem(t, listed.Data, "empty")
		if emptyItem.Status != "ready" || emptyItem.Tools == nil || len(emptyItem.Tools) != 0 {
			t.Fatalf("saved empty catalog = %#v, want ready/[]", emptyItem)
		}
	})

	t.Run("another Agent reuses the global endpoint snapshot", func(t *testing.T) {
		other := createAgent(t, app, fmt.Sprintf(`{
			"model":"claude-opus-4-6",
			"name":"shared-mcp-catalog-agent",
			"mcp_servers":[{"name":"shared_weather","type":"url","url":%q}]
			}`, weatherEndpoint))
		defer cleanupAgentRows(t, app.db, other.ID)

		otherPath := basePath + "default/agents/" + other.ID + "/mcp_tool_catalogs"
		listed := getTestMCPCatalogs(t, app, otherPath, cookies)
		item := findTestCatalogItem(t, listed.Data, "shared_weather")
		if item.Status != "ready" || len(item.Tools) != 1 || item.Tools[0].Name != "get_forecast" {
			t.Fatalf("shared endpoint catalog = %#v", item)
		}
		var count int
		if err := app.db.Pool.QueryRow(context.Background(), `
			select count(*) from mcp_tool_catalogs
			where transport_type = 'url' and endpoint_url = $1
		`, weatherEndpoint).Scan(&count); err != nil {
			t.Fatalf("count shared catalogs: %v", err)
		}
		if count != 1 {
			t.Fatalf("shared endpoint catalog rows = %d, want 1", count)
		}
	})
}

func newControllableMCPServer(t *testing.T, tools ...*mcp.Tool) *controllableMCPServer {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-mcp", Version: "1.0.0"}, nil)
	for _, tool := range tools {
		mcp.AddTool(server, tool, testMCPToolHandler)
	}
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	controlled := &controllableMCPServer{}
	controlled.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch controlled.mode.Load() {
		case testMCPUnavailable:
			http.Error(w, "upstream body must not be exposed", http.StatusServiceUnavailable)
			return
		default:
			mcpHandler.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(controlled.server.Close)
	return controlled
}

func testMCPToolHandler(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return nil, nil, nil
}

func refreshTestMCPCatalog(t *testing.T, app *testApp, agentPath, serverName string, cookies []*http.Cookie) *http.Response {
	t.Helper()
	body, err := json.Marshal(map[string]string{"server_name": serverName})
	if err != nil {
		t.Fatalf("marshal refresh body: %v", err)
	}
	return app.platformRequest(t, http.MethodPost, agentPath+"/refresh?version=1", strings.NewReader(string(body)), cookies)
}

func getTestMCPCatalogs(t *testing.T, app *testApp, path string, cookies []*http.Cookie) testCatalogListResponse {
	t.Helper()
	resp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("catalog GET status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var payload testCatalogListResponse
	decodeJSON(t, resp.Body, &payload)
	return payload
}

func findTestCatalogItem(t *testing.T, items []testCatalogItem, serverName string) testCatalogItem {
	t.Helper()
	for _, item := range items {
		if item.ServerName == serverName {
			return item
		}
	}
	t.Fatalf("catalog response does not contain server %q: %#v", serverName, items)
	return testCatalogItem{}
}

func deleteTestMCPCatalogByEndpoint(t *testing.T, database *db.DB, endpointURL string) {
	t.Helper()
	normalized, err := mcpcatalogs.NormalizeEndpoint(endpointURL)
	if err != nil {
		t.Fatalf("normalize MCP catalog cleanup endpoint: %v", err)
	}
	if _, err := database.Pool.Exec(context.Background(), `
			delete from mcp_tool_catalogs where transport_type = 'url' and endpoint_url = $1
		`, normalized); err != nil {
		t.Errorf("delete MCP catalog: %v", err)
	}
}
