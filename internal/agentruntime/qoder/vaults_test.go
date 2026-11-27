package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestQoderVaultsCredentialsSessionMappingAndIDOR(t *testing.T) {
	now := time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC)
	var upstreamHits atomic.Int32
	var lastCredUpdateBody map[string]any
	var lastSessionVaultIDs []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/vaults":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vlt_remote_1",
				"display_name": req["display_name"],
				"metadata":     req["metadata"],
				"created_at":   now,
				"updated_at":   now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/vaults/vlt_remote_1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vlt_remote_1",
				"display_name": "Prod Vault",
				"metadata":     map[string]string{watermarkOrgKey: "org-a", watermarkWorkspaceKey: "ws-a", "env": "prod"},
				"created_at":   now,
				"updated_at":   now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/vaults/vlt_remote_1/archive":
			archived := now.Add(time.Minute)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vlt_remote_1",
				"display_name": "Prod Vault",
				"archived_at":  archived,
				"created_at":   now,
				"updated_at":   archived,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/vaults/vlt_remote_1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/vaults/vlt_remote_1/credentials":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vcrd_remote_1",
				"vault_id":     "vlt_remote_1",
				"display_name": req["display_name"],
				"metadata":     req["metadata"],
				"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com"},
				"created_at":   now,
				"updated_at":   now,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/vaults/vlt_remote_1/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":           "vcrd_remote_1",
						"vault_id":     "vlt_remote_1",
						"display_name": "API Token",
						"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com"},
						"created_at":   now,
						"updated_at":   now,
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/vaults/vlt_remote_1/credentials/vcrd_remote_1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vcrd_remote_1",
				"vault_id":     "vlt_remote_1",
				"display_name": "API Token",
				"metadata":     map[string]string{"owner": "sec"},
				"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com"},
				"created_at":   now,
				"updated_at":   now,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/vaults/vlt_remote_1/credentials/vcrd_remote_1":
			_ = json.NewDecoder(r.Body).Decode(&lastCredUpdateBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vcrd_remote_1",
				"vault_id":     "vlt_remote_1",
				"display_name": "API Token",
				"metadata":     lastCredUpdateBody["metadata"],
				"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com"},
				"created_at":   now,
				"updated_at":   now.Add(time.Second),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/vaults/vlt_remote_1/credentials/vcrd_remote_1/archive":
			archived := now.Add(2 * time.Second)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           "vcrd_remote_1",
				"vault_id":     "vlt_remote_1",
				"display_name": "API Token",
				"archived_at":  archived,
				"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com"},
				"created_at":   now,
				"updated_at":   archived,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/vaults/vlt_remote_1/credentials/vcrd_remote_1":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			var req struct {
				VaultIDs []string `json:"vault_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			lastSessionVaultIDs = req.VaultIDs
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "sesn_remote_v1",
				"agent_id":       "agent_remote_1",
				"environment_id": "env_remote_1",
				"vault_ids":      req.VaultIDs,
				"status":         "idle",
				"created_at":     now,
				"updated_at":     now,
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	rt, _, _ := newTestRuntime(t, srv.URL, nil)
	ctx := context.Background()
	scopeA := agentruntime.Scope{
		OrganizationUUID:   "org-a",
		WorkspaceUUID:      "ws-a",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}
	scopeB := agentruntime.Scope{
		OrganizationUUID:   "org-b",
		WorkspaceUUID:      "ws-b",
		RuntimeProfileUUID: rt.profile.UUID,
		ProviderKind:       agentruntime.ProviderQoderCloud,
	}

	vault, err := rt.CreateVault(ctx, scopeA, agentruntime.CreateVaultInput{
		DisplayName: "Prod Vault",
		Metadata:    map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, hasWatermark := vault.Metadata[watermarkOrgKey]; hasWatermark {
		t.Fatalf("watermark leaked: %+v", vault.Metadata)
	}

	hitsBeforeIDOR := upstreamHits.Load()
	if _, err := rt.GetVault(ctx, scopeB, vault.ID); !errors.Is(err, agentruntime.ErrResourceNotFound) {
		t.Fatalf("cross-tenant GetVault err = %v, want ErrResourceNotFound", err)
	}
	if upstreamHits.Load() != hitsBeforeIDOR {
		t.Fatalf("cross-tenant GetVault must not hit upstream")
	}

	_, updateErr := rt.UpdateVault(ctx, scopeA, vault.ID, agentruntime.UpdateVaultInput{})
	var appErr *apperr.Error
	if !errors.As(updateErr, &appErr) || appErr.Kind != apperr.NotImplemented {
		t.Fatalf("UpdateVault err = %v, want apperr.NotImplemented", updateErr)
	}

	cred, err := rt.CreateVaultCredential(ctx, scopeA, vault.ID, agentruntime.CreateVaultCredentialInput{
		DisplayName: "API Token",
		Metadata:    map[string]string{"owner": "sec"},
		Auth:        json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com","token":"secret"}`),
	})
	if err != nil || cred.VaultID != vault.ID {
		t.Fatalf("CreateVaultCredential = (%+v, %v)", cred, err)
	}

	ignoredName := "New Name"
	updatedCred, err := rt.UpdateVaultCredential(ctx, scopeA, vault.ID, cred.ID, agentruntime.UpdateVaultCredentialInput{
		DisplayName: &ignoredName,
		Metadata:    map[string]string{"owner": "platform"},
		MetadataSet: true,
	})
	if err != nil || updatedCred.VaultID != vault.ID {
		t.Fatalf("UpdateVaultCredential = (%+v, %v)", updatedCred, err)
	}
	if _, sentDisplayName := lastCredUpdateBody["display_name"]; sentDisplayName {
		t.Fatalf("UpdateVaultCredential must omit display_name for qoder: %+v", lastCredUpdateBody)
	}

	credsPage, err := rt.ListVaultCredentials(ctx, scopeA, vault.ID, agentruntime.ListVaultCredentialsQuery{Limit: 10})
	if err != nil || len(credsPage.Items) != 1 || credsPage.Items[0].VaultID != vault.ID {
		t.Fatalf("ListVaultCredentials = (%+v, %v)", credsPage, err)
	}

	_, _ = rt.bindings.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeAgent,
		OMAExternalID: "agent_oma_1",
		RemoteID:      "agent_remote_1",
	}, json.RawMessage(`{}`))
	_, _ = rt.bindings.Bind(ctx, scopeA, agentruntime.ResourceRef{
		Type:          agentruntime.ResourceTypeEnvironment,
		OMAExternalID: "env_oma_1",
		RemoteID:      "env_remote_1",
	}, json.RawMessage(`{}`))
	sess, err := rt.CreateSession(ctx, scopeA, agentruntime.CreateSessionInput{
		AgentID:       "agent_oma_1",
		EnvironmentID: "env_oma_1",
		VaultIDs:      []string{vault.ID},
	})
	if err != nil {
		t.Fatalf("CreateSession with vault: %v", err)
	}
	if len(lastSessionVaultIDs) != 1 || lastSessionVaultIDs[0] != "vlt_remote_1" {
		t.Fatalf("expected remote vault id sent to session create: %+v", lastSessionVaultIDs)
	}
	if len(sess.VaultIDs) != 1 || sess.VaultIDs[0] != vault.ID {
		t.Fatalf("expected session vault_ids mapped back to oma id: %+v", sess.VaultIDs)
	}

	if _, err := rt.ArchiveVaultCredential(ctx, scopeA, vault.ID, cred.ID); err != nil {
		t.Fatalf("ArchiveVaultCredential: %v", err)
	}
	if err := rt.DeleteVaultCredential(ctx, scopeA, vault.ID, cred.ID); err != nil {
		t.Fatalf("DeleteVaultCredential: %v", err)
	}
	if _, err := rt.ArchiveVault(ctx, scopeA, vault.ID); err != nil {
		t.Fatalf("ArchiveVault: %v", err)
	}
	if err := rt.DeleteVault(ctx, scopeA, vault.ID); err != nil {
		t.Fatalf("DeleteVault: %v", err)
	}
}
