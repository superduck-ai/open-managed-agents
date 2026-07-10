package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMCPToolCatalogDatabaseTransitions(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()
	ctx := context.Background()

	t.Run("failure settles a leased generation", func(t *testing.T) {
		endpointKey := "mcpe_test_" + uuid.NewString()
		ensured := ensureTestMCPCatalog(t, app.db, endpointKey)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		job := leaseTestMCPJob(t, app.db, ensured.Catalog.ExternalID, "mcp-test-failure")
		now := time.Now().UTC()
		if err := app.db.FailMCPToolDiscovery(ctx, db.FailMCPToolDiscoveryInput{
			JobID:             job.ID,
			WorkerID:          "mcp-test-failure",
			WorkspaceID:       job.WorkspaceID,
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
		catalog, err := app.db.GetMCPToolCatalog(ctx, 9001, 9002, endpointKey, "anonymous")
		if err != nil {
			t.Fatalf("GetMCPToolCatalog: %v", err)
		}
		if catalog.SettledGeneration != 1 || valueOrEmpty(catalog.LastResultStatus) != "auth_required" {
			t.Fatalf("settled catalog = %#v", catalog)
		}
	})

	t.Run("active catalog polling avoids repeated reference writes", func(t *testing.T) {
		endpointKey := "mcpe_test_" + uuid.NewString()
		ensured := ensureTestMCPCatalog(t, app.db, endpointKey)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		polledAt := ensured.Catalog.LastReferencedAt.Add(time.Second)
		polled, err := app.db.EnsureMCPToolCatalog(ctx, db.EnsureMCPToolCatalogInput{
			OrganizationID: 9001,
			WorkspaceID:    9002,
			TransportType:  "url",
			EndpointURL:    "https://example.test/mcp",
			EndpointKey:    endpointKey,
			AuthScopeKey:   "anonymous",
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
		endpointKey := "mcpe_test_" + uuid.NewString()
		ensured := ensureTestMCPCatalog(t, app.db, endpointKey)
		defer deleteTestMCPCatalog(t, app.db, ensured.Catalog.ExternalID)
		job := leaseTestMCPJob(t, app.db, ensured.Catalog.ExternalID, "mcp-test-success")
		now := time.Now().UTC()
		if err := app.db.CompleteMCPToolDiscovery(ctx, db.CompleteMCPToolDiscoveryInput{
			JobID:             job.ID,
			WorkerID:          "mcp-test-success",
			WorkspaceID:       job.WorkspaceID,
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
		catalog, err := app.db.GetMCPToolCatalog(ctx, 9001, 9002, endpointKey, "anonymous")
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
}

func ensureTestMCPCatalog(t *testing.T, database *db.DB, endpointKey string) db.EnsureMCPToolCatalogResult {
	t.Helper()
	result, err := database.EnsureMCPToolCatalog(context.Background(), db.EnsureMCPToolCatalogInput{
		OrganizationID: 9001,
		WorkspaceID:    9002,
		TransportType:  "url",
		EndpointURL:    "https://example.test/mcp",
		EndpointKey:    endpointKey,
		AuthScopeKey:   "anonymous",
		Trigger:        "test",
		Now:            time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("EnsureMCPToolCatalog: %v", err)
	}
	return result
}

func leaseTestMCPJob(t *testing.T, database *db.DB, catalogExternalID, workerID string) db.MCPToolDiscoveryJob {
	t.Helper()
	var job db.MCPToolDiscoveryJob
	err := database.Pool.QueryRow(context.Background(), `
		update jobs
		set status = 'running', locked_by = $2, locked_until = now() + interval '1 minute', updated_at = now()
		where type = 'mcp_tool_discovery'
			and payload->>'catalog_external_id' = $1
		returning id, workspace_id, attempts, payload, (payload->>'generation')::bigint
	`, catalogExternalID, workerID).Scan(&job.ID, &job.WorkspaceID, &job.Attempts, &job.Payload, &job.Generation)
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

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
