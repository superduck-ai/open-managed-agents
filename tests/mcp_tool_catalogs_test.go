package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMCPToolCatalogConsoleAuthorization(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("mcp-catalog-console-bucket"))
	defer app.close()
	endpointURL := testMCPEndpointURL()
	agent := createAgent(t, app, `{
		"model":"claude-opus-4-6",
		"name":"mcp-catalog-console-agent",
		"mcp_servers":[{"name":"weather","type":"url","url":"`+endpointURL+`"}]
	}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	defer deleteTestMCPCatalogByEndpoint(t, app.db, endpointURL)

	cookies := app.platformLoginCookies(t, "mcp-catalog-console@example.com")
	orgCookie := responseCookie(cookies, "lastActiveOrg")
	if orgCookie == nil {
		t.Fatal("platform login did not return lastActiveOrg")
	}
	basePath := "/api/console/organizations/" + orgCookie.Value + "/workspaces/"

	t.Run("rejects a workspace outside the authenticated Agent scope", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, basePath+"other/agents/"+agent.ID+"/mcp_tool_catalogs", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-workspace status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("rejects refresh URLs supplied outside the Agent configuration", func(t *testing.T) {
		body := strings.NewReader(`{"server_names":["weather"],"url":"https://attacker.example/mcp"}`)
		resp := app.platformRequest(t, http.MethodPost, basePath+"default/agents/"+agent.ID+"/mcp_tool_catalogs/refresh", body, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("arbitrary refresh URL status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("returns the Agent-scoped view without exposing endpoint identity", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, basePath+"default/agents/"+agent.ID+"/mcp_tool_catalogs?version=1", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("catalog status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		decodeJSON(t, resp.Body, &payload)
		if len(payload.Data) != 1 || payload.Data[0]["server_name"] != "weather" {
			t.Fatalf("catalog response = %#v", payload.Data)
		}
		if _, exposed := payload.Data[0]["endpoint_fingerprint"]; exposed {
			t.Fatalf("catalog response exposed endpoint identity: %#v", payload.Data[0])
		}
	})
}

func TestMCPToolCatalogDatabaseTransitions(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()
	ctx := context.Background()

	t.Run("failure settles a leased generation", func(t *testing.T) {
		endpointURL := testMCPEndpointURL()
		ensured := ensureTestMCPCatalog(t, app.db, 9002, endpointURL)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		job := leaseTestMCPJob(t, app.db, ensured.Catalog.ExternalID, "mcp-test-failure")
		now := time.Now().UTC()
		if err := app.db.FailMCPToolDiscovery(ctx, db.FailMCPToolDiscoveryInput{
			JobID:             job.ID,
			WorkerID:          "mcp-test-failure",
			CatalogExternalID: job.CatalogExternalID,
			Generation:        job.Generation,
			MaxAttempts:       4,
			Retryable:         false,
			RetryDelay:        time.Hour,
			ErrorCode:         "auth_required",
			ErrorMessage:      "Authentication is required.",
			Now:               now,
		}); err != nil {
			t.Fatalf("FailMCPToolDiscovery: %v", err)
		}
		catalog, err := app.db.GetMCPToolCatalog(ctx, "url", endpointURL)
		if err != nil {
			t.Fatalf("GetMCPToolCatalog: %v", err)
		}
		if catalog.SettledGeneration != 1 || valueOrEmpty(catalog.LastResultStatus) != "auth_required" {
			t.Fatalf("settled catalog = %#v", catalog)
		}
	})

	t.Run("active catalog polling avoids repeated reference writes", func(t *testing.T) {
		endpointURL := testMCPEndpointURL()
		ensured := ensureTestMCPCatalog(t, app.db, 9002, endpointURL)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		polledAt := ensured.Catalog.LastReferencedAt.Add(time.Second)
		polled, err := app.db.EnsureMCPToolCatalog(ctx, db.EnsureMCPToolCatalogInput{
			JobWorkspaceID: 9002,
			TransportType:  "url",
			EndpointURL:    endpointURL,
			Trigger:        "detail_read",
			Now:            polledAt,
		})
		if err != nil {
			t.Fatalf("poll EnsureMCPToolCatalog: %v", err)
		}
		if polled.Queued {
			t.Fatal("active catalog poll unexpectedly queued another generation")
		}
		if !polled.Catalog.LastReferencedAt.Equal(ensured.Catalog.LastReferencedAt) || !polled.Catalog.UpdatedAt.Equal(ensured.Catalog.UpdatedAt) {
			t.Fatalf("active poll wrote catalog timestamps: before=%v/%v after=%v/%v",
				ensured.Catalog.LastReferencedAt, ensured.Catalog.UpdatedAt,
				polled.Catalog.LastReferencedAt, polled.Catalog.UpdatedAt,
			)
		}
	})

	t.Run("success stores a confirmed empty catalog and retention runs", func(t *testing.T) {
		endpointURL := testMCPEndpointURL()
		ensured := ensureTestMCPCatalog(t, app.db, 9002, endpointURL)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		job := leaseTestMCPJob(t, app.db, ensured.Catalog.ExternalID, "mcp-test-success")
		now := time.Now().UTC()
		if err := app.db.CompleteMCPToolDiscovery(ctx, db.CompleteMCPToolDiscoveryInput{
			JobID:             job.ID,
			WorkerID:          "mcp-test-success",
			CatalogExternalID: job.CatalogExternalID,
			Generation:        job.Generation,
			Tools:             json.RawMessage(`[]`),
			ProtocolVersion:   "2025-03-26",
			ServerInfo:        json.RawMessage(`{"name":"test"}`),
			CatalogHash:       "empty-hash",
			DiscoveredAt:      now,
			ExpiresAt:         now.Add(10 * time.Minute),
		}); err != nil {
			t.Fatalf("CompleteMCPToolDiscovery: %v", err)
		}
		catalog, err := app.db.GetMCPToolCatalog(ctx, "url", endpointURL)
		if err != nil {
			t.Fatalf("GetMCPToolCatalog: %v", err)
		}
		if catalog.SettledGeneration != 1 || valueOrEmpty(catalog.LastResultStatus) != "success" || string(catalog.Tools) != "[]" {
			t.Fatalf("completed catalog = %#v", catalog)
		}
		if err := app.db.RunMCPToolCatalogRetention(ctx, now); err != nil {
			t.Fatalf("RunMCPToolCatalogRetention: %v", err)
		}
	})

	t.Run("same endpoint shares one active catalog across workspaces", func(t *testing.T) {
		endpointURL := testMCPEndpointURL()
		first := ensureTestMCPCatalog(t, app.db, 9002, endpointURL)
		defer deleteTestMCPCatalog(t, app.db, first.Catalog.ExternalID)
		second := ensureTestMCPCatalog(t, app.db, 9003, endpointURL)
		if first.Catalog.ExternalID != second.Catalog.ExternalID {
			t.Fatalf("catalog IDs differ across workspaces: %q != %q", first.Catalog.ExternalID, second.Catalog.ExternalID)
		}
		if !first.Queued || second.Queued {
			t.Fatalf("queue results = first:%v second:%v, want true/false", first.Queued, second.Queued)
		}
		var catalogs, jobs int
		if err := app.db.Pool.QueryRow(ctx, `select count(*) from mcp_tool_catalogs where transport_type = 'url' and endpoint_url = $1`, endpointURL).Scan(&catalogs); err != nil {
			t.Fatalf("count shared catalogs: %v", err)
		}
		if err := app.db.Pool.QueryRow(ctx, `select count(*) from jobs where type = 'mcp_tool_discovery' and payload->>'catalog_external_id' = $1`, first.Catalog.ExternalID).Scan(&jobs); err != nil {
			t.Fatalf("count shared catalog jobs: %v", err)
		}
		if catalogs != 1 || jobs != 1 {
			t.Fatalf("shared endpoint rows = catalogs:%d jobs:%d, want 1/1", catalogs, jobs)
		}
	})
}

func ensureTestMCPCatalog(t *testing.T, database *db.DB, workspaceID int64, endpointURL string) db.EnsureMCPToolCatalogResult {
	t.Helper()
	result, err := database.EnsureMCPToolCatalog(context.Background(), db.EnsureMCPToolCatalogInput{
		JobWorkspaceID: workspaceID,
		TransportType:  "url",
		EndpointURL:    endpointURL,
		Trigger:        "test",
		Now:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("EnsureMCPToolCatalog: %v", err)
	}
	return result
}

func testMCPEndpointURL() string {
	return "https://example.test/" + uuid.NewString() + "/mcp"
}

func leaseTestMCPJob(t *testing.T, database *db.DB, catalogExternalID, workerID string) db.MCPToolDiscoveryJob {
	t.Helper()
	var job db.MCPToolDiscoveryJob
	var workspaceID int64
	var payload json.RawMessage
	err := database.Pool.QueryRow(context.Background(), `
		update jobs
		set status = 'running', locked_by = $2, locked_until = now() + interval '1 minute', updated_at = now()
		where type = 'mcp_tool_discovery'
			and payload->>'catalog_external_id' = $1
		returning id, workspace_id, attempts, payload, (payload->>'generation')::bigint
	`, catalogExternalID, workerID).Scan(&job.ID, &workspaceID, &job.Attempts, &payload, &job.Generation)
	if err != nil {
		t.Fatalf("lease test MCP job: %v", err)
	}
	job.CatalogExternalID = catalogExternalID
	return job
}

func deleteTestMCPCatalog(t *testing.T, database *db.DB, catalogExternalID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := database.Pool.Exec(ctx, `delete from jobs where type = 'mcp_tool_discovery' and payload->>'catalog_external_id' = $1`, catalogExternalID); err != nil {
		t.Errorf("delete MCP discovery jobs: %v", err)
	}
	if _, err := database.Pool.Exec(ctx, `delete from mcp_tool_catalogs where external_id = $1`, catalogExternalID); err != nil {
		t.Errorf("delete MCP catalog: %v", err)
	}
}

func deleteTestMCPCatalogByEndpoint(t *testing.T, database *db.DB, endpointURL string) {
	t.Helper()
	ctx := context.Background()
	var catalogExternalID string
	err := database.Pool.QueryRow(ctx, `select external_id from mcp_tool_catalogs where transport_type = 'url' and endpoint_url = $1`, endpointURL).Scan(&catalogExternalID)
	if err != nil {
		t.Errorf("find MCP catalog for cleanup: %v", err)
		return
	}
	deleteTestMCPCatalog(t, database, catalogExternalID)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
