package vaults

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type fakeVaultProfileStore struct {
	profile db.RuntimeProfile
}

func (f *fakeVaultProfileStore) GetRuntimeProfileByUUID(_ context.Context, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeVaultProfileStore) GetWorkspaceRuntimeProfile(_ context.Context, _, _ string) (db.RuntimeProfile, error) {
	return f.profile, nil
}

func (f *fakeVaultProfileStore) UpdateRuntimeProfileHealth(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}

type fakeExternalVaultRuntime struct {
	vault         agentruntime.Vault
	cred          agentruntime.VaultCredential
	lastCredInput agentruntime.UpdateVaultCredentialInput
}

func (f *fakeExternalVaultRuntime) ProviderKind() agentruntime.ProviderKind {
	return agentruntime.ProviderQoderCloud
}

func (f *fakeExternalVaultRuntime) Capabilities() agentruntime.CapabilitySet {
	return agentruntime.DefaultCapabilities(agentruntime.ProviderQoderCloud)
}

func (f *fakeExternalVaultRuntime) Close() error { return nil }

func (f *fakeExternalVaultRuntime) CreateVault(_ context.Context, _ agentruntime.Scope, _ agentruntime.CreateVaultInput) (agentruntime.Vault, error) {
	return f.vault, nil
}

func (f *fakeExternalVaultRuntime) GetVault(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Vault, error) {
	return f.vault, nil
}

func (f *fakeExternalVaultRuntime) ListVaults(_ context.Context, _ agentruntime.Scope, _ agentruntime.ListVaultsQuery) (agentruntime.Page[agentruntime.Vault], error) {
	return agentruntime.Page[agentruntime.Vault]{Items: []agentruntime.Vault{f.vault}}, nil
}

func (f *fakeExternalVaultRuntime) UpdateVault(_ context.Context, _ agentruntime.Scope, _ string, _ agentruntime.UpdateVaultInput) (agentruntime.Vault, error) {
	return agentruntime.Vault{}, apperr.New(apperr.NotImplemented, "Updating vaults is not available on qoder runtime", nil)
}

func (f *fakeExternalVaultRuntime) ArchiveVault(_ context.Context, _ agentruntime.Scope, _ string) (agentruntime.Vault, error) {
	now := time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)
	archived := f.vault
	archived.ArchivedAt = &now
	return archived, nil
}

func (f *fakeExternalVaultRuntime) DeleteVault(_ context.Context, _ agentruntime.Scope, _ string) error {
	return nil
}

func (f *fakeExternalVaultRuntime) CreateVaultCredential(_ context.Context, _ agentruntime.Scope, _ string, _ agentruntime.CreateVaultCredentialInput) (agentruntime.VaultCredential, error) {
	return f.cred, nil
}

func (f *fakeExternalVaultRuntime) GetVaultCredential(_ context.Context, _ agentruntime.Scope, _, _ string) (agentruntime.VaultCredential, error) {
	return f.cred, nil
}

func (f *fakeExternalVaultRuntime) ListVaultCredentials(_ context.Context, _ agentruntime.Scope, _ string, _ agentruntime.ListVaultCredentialsQuery) (agentruntime.Page[agentruntime.VaultCredential], error) {
	return agentruntime.Page[agentruntime.VaultCredential]{Items: []agentruntime.VaultCredential{f.cred}}, nil
}

func (f *fakeExternalVaultRuntime) UpdateVaultCredential(_ context.Context, _ agentruntime.Scope, _, _ string, input agentruntime.UpdateVaultCredentialInput) (agentruntime.VaultCredential, error) {
	f.lastCredInput = input
	return f.cred, nil
}

func (f *fakeExternalVaultRuntime) ArchiveVaultCredential(_ context.Context, _ agentruntime.Scope, _, _ string) (agentruntime.VaultCredential, error) {
	now := time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)
	archived := f.cred
	archived.ArchivedAt = &now
	return archived, nil
}

func (f *fakeExternalVaultRuntime) DeleteVaultCredential(_ context.Context, _ agentruntime.Scope, _, _ string) error {
	return nil
}

func TestExternalVaultsAndCredentialsDispatch(t *testing.T) {
	now := time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC)
	fakeRT := &fakeExternalVaultRuntime{
		vault: agentruntime.Vault{
			ID:          "vlt_ext_1",
			DisplayName: "Prod Vault",
			Metadata:    map[string]string{"env": "prod"},
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		cred: agentruntime.VaultCredential{
			ID:          "vcrd_ext_1",
			VaultID:     "vlt_ext_1",
			DisplayName: "API Token",
			Metadata:    map[string]string{"owner": "sec"},
			Auth:        json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com"}`),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}

	reg := agentruntime.NewRegistry()
	if err := reg.Register(agentruntime.ProviderQoderCloud, func(_ context.Context, _ agentruntime.ProfileRuntimeConfig) (agentruntime.Runtime, error) {
		return fakeRT, nil
	}); err != nil {
		t.Fatal(err)
	}
	resolver := agentruntime.NewResolver(&fakeVaultProfileStore{
		profile: db.RuntimeProfile{
			UUID:         "00000000-0000-4000-8000-000000000077",
			ProviderKind: agentruntime.ProviderQoderCloud.String(),
			Status:       "active",
		},
	}, reg, nil, nil)

	handler := NewHandler(config.Config{}, nil, nil, nil, nil).WithRuntimeResolver(resolver)
	principal := auth.Principal{
		OrganizationUUID: "org-1",
		WorkspaceUUID:    "ws-1",
		CredentialType:   auth.CredentialTypeAPIKey,
		RuntimeProvider:  agentruntime.ProviderQoderCloud.String(),
	}

	doReq := func(method, target string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, target, &buf)
		req = req.WithContext(auth.WithPrincipal(req.Context(), principal))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := doReq(http.MethodPost, "/", map[string]any{"display_name": "Prod Vault", "metadata": map[string]string{"env": "prod"}}); rec.Code != http.StatusOK {
		t.Fatalf("create vault status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/", nil); rec.Code != http.StatusOK {
		t.Fatalf("list vaults status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/vlt_ext_1", nil); rec.Code != http.StatusOK {
		t.Fatalf("get vault status = %d body = %s", rec.Code, rec.Body.String())
	}
	rec := doReq(http.MethodPost, "/vlt_ext_1", map[string]any{"display_name": "Renamed"})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("update vault status = %d, want 501: %s", rec.Code, rec.Body.String())
	}
	var errResp struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil || errResp.Error.Type != "feature_not_available" {
		t.Fatalf("update vault error = %s", rec.Body.String())
	}

	if rec := doReq(http.MethodPost, "/vlt_ext_1/credentials", map[string]any{
		"display_name": "API Token",
		"auth":         map[string]any{"type": "static_bearer", "mcp_server_url": "https://mcp.example.com", "token": "secret"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("create credential status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/vlt_ext_1/credentials", nil); rec.Code != http.StatusOK {
		t.Fatalf("list credentials status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodGet, "/vlt_ext_1/credentials/vcrd_ext_1", nil); rec.Code != http.StatusOK {
		t.Fatalf("get credential status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/vlt_ext_1/credentials/vcrd_ext_1", map[string]any{
		"metadata": map[string]any{"owner": nil, "tier": "ops"},
	}); rec.Code != http.StatusOK {
		t.Fatalf("update credential status = %d body = %s", rec.Code, rec.Body.String())
	}
	if fakeRT.lastCredInput.Metadata["tier"] != "ops" || fakeRT.lastCredInput.Metadata["owner"] != "" {
		t.Fatalf("unexpected patched metadata: %+v", fakeRT.lastCredInput.Metadata)
	}
	if rec := doReq(http.MethodPost, "/vlt_ext_1/credentials/vcrd_ext_1/mcp_oauth_validate", nil); rec.Code != http.StatusNotImplemented {
		t.Fatalf("validate credential status = %d, want 501: %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/vlt_ext_1/credentials/vcrd_ext_1/archive", nil); rec.Code != http.StatusOK {
		t.Fatalf("archive credential status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodDelete, "/vlt_ext_1/credentials/vcrd_ext_1", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete credential status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodPost, "/vlt_ext_1/archive", nil); rec.Code != http.StatusOK {
		t.Fatalf("archive vault status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec := doReq(http.MethodDelete, "/vlt_ext_1", nil); rec.Code != http.StatusOK {
		t.Fatalf("delete vault status = %d body = %s", rec.Code, rec.Body.String())
	}
}
