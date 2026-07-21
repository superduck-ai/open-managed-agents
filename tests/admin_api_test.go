package tests

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/google/uuid"
)

const tunnelsBetaHeader = "mcp-tunnels-2026-05-19"

type adminObject struct {
	ID             string            `json:"id"`
	Type           string            `json:"type"`
	Name           string            `json:"name"`
	Email          string            `json:"email"`
	Role           string            `json:"role"`
	Status         string            `json:"status"`
	WorkspaceID    *string           `json:"workspace_id"`
	UserID         string            `json:"user_id"`
	WorkspaceRole  string            `json:"workspace_role"`
	ExternalKeyID  *string           `json:"external_key_id"`
	DisplayName    string            `json:"display_name"`
	ProviderConfig json.RawMessage   `json:"provider_config"`
	Domain         string            `json:"domain"`
	TunnelToken    string            `json:"tunnel_token"`
	Fingerprint    string            `json:"fingerprint"`
	ArchivedAt     *string           `json:"archived_at"`
	Tags           map[string]string `json:"tags"`
}

type adminCursorPage struct {
	Data    []adminObject `json:"data"`
	FirstID *string       `json:"first_id"`
	HasMore bool          `json:"has_more"`
	LastID  *string       `json:"last_id"`
}

type adminTokenPage struct {
	Data     []adminObject `json:"data"`
	NextPage *string       `json:"next_page"`
}

type adminReportPage struct {
	Data     []any   `json:"data"`
	HasMore  bool    `json:"has_more"`
	NextPage *string `json:"next_page"`
}

type adminDefaultIDs struct {
	OrganizationID int64
	WorkspaceID    int64
	UserID         int64
}

func TestAdminAPI(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()

	suffix := uniqueAdminSuffix()

	t.Run("failure missing api key", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, "", "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure invalid api key", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, "sk-ant-invalid", "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure invite cannot grant admin", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/invites", map[string]any{
			"email": "admin-invite-" + suffix + "@example.com",
			"role":  "admin",
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure workspace rejects anthropic tag", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces", map[string]any{
			"name": "bad-tags-" + suffix,
			"tags": map[string]string{
				"anthropic_owner": "local",
			},
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure api key rejects unknown status", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/api_keys/api_key_default", map[string]any{
			"status": "paused",
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure rate limits unknown model", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/rate_limits?model=claude-local-missing", nil, defaultTestKey, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure tunnel requires beta header", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/tunnels", nil, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure tunnel certificate rejects invalid pem", func(t *testing.T) {
		tunnelID := seedAdminTunnel(t, app.db, "tunnel_bad_cert_"+suffix, "bad-cert-"+suffix+".local", nil)
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/certificates", map[string]any{
			"ca_certificate_pem": "not a certificate",
		}, defaultTestKey, tunnelsBetaHeader)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure cross organization isolation", func(t *testing.T) {
		otherKey := "sk-ant-admin-other-" + suffix
		seedWorkspaceKey(t, app.db, "org_admin_other_"+suffix, "workspace_admin_other_"+suffix, "api_key_admin_other_"+suffix, otherKey)
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/workspaces/workspace_default", nil, otherKey, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("success organization me", func(t *testing.T) {
		var org adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, defaultTestKey, ""), &org)
		if org.ID != "org_default" || org.Type != "organization" {
			t.Fatalf("organization = %+v, want org_default organization", org)
		}
	})

	t.Run("success invites paginate and soft delete", func(t *testing.T) {
		first := createAdminInvite(t, app, "one-"+suffix+"@example.com", "user")
		second := createAdminInvite(t, app, "two-"+suffix+"@example.com", "developer")
		forceInviteTimes(t, app.db, first.ID, second.ID)

		var page adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/invites?limit=1", nil, defaultTestKey, ""), &page)
		if len(page.Data) != 1 || page.Data[0].ID != second.ID || !page.HasMore || page.LastID == nil {
			t.Fatalf("first invite page = %+v, want latest invite %s with has_more", page, second.ID)
		}

		var next adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/invites?limit=1&after_id="+*page.LastID, nil, defaultTestKey, ""), &next)
		if len(next.Data) != 1 || next.Data[0].ID != first.ID {
			t.Fatalf("second invite page = %+v, want invite %s", next, first.ID)
		}

		var deleted adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodDelete, "/v1/organizations/invites/"+first.ID, nil, defaultTestKey, ""), &deleted)
		if deleted.ID != first.ID || deleted.Type != "invite_deleted" {
			t.Fatalf("deleted invite = %+v", deleted)
		}
	})

	t.Run("success users and workspace members", func(t *testing.T) {
		userID := seedAdminUser(t, app.db, "member-"+suffix+"@example.com", "developer")

		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/users/"+userID, map[string]any{"role": "admin"}, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		var user adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/users/"+userID, map[string]any{"role": "claude_code_user"}, defaultTestKey, ""), &user)
		if user.Role != "claude_code_user" {
			t.Fatalf("updated user role = %s", user.Role)
		}

		workspace := createAdminWorkspace(t, app, "members-"+suffix, nil, map[string]string{"team": "admin"})
		resp = adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/members", map[string]any{
			"user_id":        userID,
			"workspace_role": "workspace_billing",
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		var member adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/members", map[string]any{
			"user_id":        userID,
			"workspace_role": "workspace_developer",
		}, defaultTestKey, ""), &member)
		if member.UserID != userID || member.WorkspaceID == nil || *member.WorkspaceID != workspace.ID || member.WorkspaceRole != "workspace_developer" {
			t.Fatalf("workspace member = %+v", member)
		}

		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/members/"+userID, map[string]any{
			"workspace_role": "workspace_billing",
		}, defaultTestKey, ""), &member)
		if member.WorkspaceRole != "workspace_billing" {
			t.Fatalf("updated workspace member role = %s", member.WorkspaceRole)
		}

		var deleted adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodDelete, "/v1/organizations/workspaces/"+workspace.ID+"/members/"+userID, nil, defaultTestKey, ""), &deleted)
		if deleted.Type != "workspace_member_deleted" || deleted.UserID != userID {
			t.Fatalf("deleted workspace member = %+v", deleted)
		}
	})

	t.Run("success workspace archive and external key protections", func(t *testing.T) {
		key := createAdminExternalKey(t, app, "primary-"+suffix)
		secondKey := createAdminExternalKey(t, app, "secondary-"+suffix)
		workspace := createAdminWorkspace(t, app, "cmek-"+suffix, &key.ID, map[string]string{"env": "test"})
		if workspace.ExternalKeyID == nil || *workspace.ExternalKeyID != key.ID || workspace.Tags["env"] != "test" {
			t.Fatalf("workspace = %+v, want external key and tags", workspace)
		}

		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID, map[string]any{
			"external_key_id": secondKey.ID,
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusConflict, "conflict_error")

		resp = adminDo(t, app, http.MethodPost, "/v1/organizations/external_keys/"+key.ID, map[string]any{
			"provider_config": map[string]any{
				"type":     "aws",
				"kms_arn":  "arn:aws:kms:us-west-2:123456789012:key/" + suffix,
				"role_arn": "arn:aws:iam::123456789012:role/demo",
			},
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusConflict, "conflict_error")

		resp = adminDo(t, app, http.MethodDelete, "/v1/organizations/external_keys/"+key.ID, nil, defaultTestKey, "")
		assertError(t, resp, http.StatusConflict, "conflict_error")

		var validation adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/external_keys/"+key.ID+"/validate", nil, defaultTestKey, ""), &validation)
		if validation.Status != "success" {
			t.Fatalf("external key validation = %+v", validation)
		}

		var archived adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces/"+workspace.ID+"/archive", nil, defaultTestKey, ""), &archived)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived workspace = %+v, want archived_at", archived)
		}

		var activePage adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/workspaces?limit=1000", nil, defaultTestKey, ""), &activePage)
		if containsAdminObject(activePage.Data, workspace.ID) {
			t.Fatalf("archived workspace %s appeared in active list", workspace.ID)
		}

		var archivedPage adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/workspaces?include_archived=true&limit=1000", nil, defaultTestKey, ""), &archivedPage)
		if !containsAdminObject(archivedPage.Data, workspace.ID) {
			t.Fatalf("archived workspace %s missing from include_archived list", workspace.ID)
		}
	})

	t.Run("success api key status update affects auth", func(t *testing.T) {
		apiKeyID, rawKey := seedAdminAPIKey(t, app.db, "status-"+suffix, "sk-ant-admin-status-"+suffix)
		pageKeyID, _ := seedAdminAPIKey(t, app.db, "page-"+suffix, "sk-ant-admin-page-"+suffix)
		forceAPIKeyTimes(t, app.db, apiKeyID, pageKeyID)

		var key adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/api_keys/"+apiKeyID, nil, defaultTestKey, ""), &key)
		if key.ID != apiKeyID || key.Status != "active" {
			t.Fatalf("api key = %+v", key)
		}

		var page adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/api_keys?limit=1", nil, defaultTestKey, ""), &page)
		if len(page.Data) != 1 || page.Data[0].ID != pageKeyID || !page.HasMore || page.LastID == nil {
			t.Fatalf("first api key page = %+v, want latest key %s with has_more", page, pageKeyID)
		}
		var next adminCursorPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/api_keys?limit=1&after_id="+*page.LastID, nil, defaultTestKey, ""), &next)
		if len(next.Data) != 1 || next.Data[0].ID != apiKeyID {
			t.Fatalf("second api key page = %+v, want key %s", next, apiKeyID)
		}

		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/api_keys/"+apiKeyID, map[string]any{
			"name":   "inactive-" + suffix,
			"status": "inactive",
		}, defaultTestKey, ""), &key)
		if key.Status != "inactive" || key.Name != "inactive-"+suffix {
			t.Fatalf("updated api key = %+v", key)
		}

		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, rawKey, "")
		assertError(t, resp, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("success reports and default rate limits are empty", func(t *testing.T) {
		var limits adminTokenPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/rate_limits", nil, defaultTestKey, ""), &limits)
		if len(limits.Data) != 0 || limits.NextPage != nil {
			t.Fatalf("rate limits = %+v, want empty page", limits)
		}

		var messages adminReportPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/usage_report/messages?starting_at=2026-01-01T00:00:00Z&bucket_width=1d", nil, defaultTestKey, ""), &messages)
		if len(messages.Data) != 0 || messages.HasMore || messages.NextPage != nil {
			t.Fatalf("messages report = %+v, want empty report", messages)
		}

		var cost adminReportPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/cost_report?starting_at=2026-01-01T00:00:00Z&bucket_width=1d", nil, defaultTestKey, ""), &cost)
		if len(cost.Data) != 0 || cost.HasMore {
			t.Fatalf("cost report = %+v, want empty report", cost)
		}
	})

	t.Run("success tunnel token certificate limits and archive", func(t *testing.T) {
		workspace := createAdminWorkspace(t, app, "tunnel-"+suffix, nil, nil)
		tunnelID := seedAdminTunnel(t, app.db, "tunnel_"+suffix, "tunnel-"+suffix+".local", &workspace.ID)

		var tunnel adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/tunnels/"+tunnelID, nil, defaultTestKey, tunnelsBetaHeader), &tunnel)
		if tunnel.ID != tunnelID || tunnel.WorkspaceID == nil || *tunnel.WorkspaceID != workspace.ID {
			t.Fatalf("tunnel = %+v", tunnel)
		}

		var token adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/reveal_token", nil, defaultTestKey, tunnelsBetaHeader), &token)
		if token.Type != "tunnel_token" || token.TunnelToken == "" {
			t.Fatalf("revealed token = %+v", token)
		}
		firstToken := token.TunnelToken

		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/rotate_token", map[string]any{"reason": "test"}, defaultTestKey, tunnelsBetaHeader), &token)
		if token.TunnelToken == "" || token.TunnelToken == firstToken {
			t.Fatalf("rotated token = %+v, want new token", token)
		}

		certPEM := newTestCertificatePEM(t, "admin-one-"+suffix)
		certPEM2 := newTestCertificatePEM(t, "admin-two-"+suffix)
		certPEM3 := newTestCertificatePEM(t, "admin-three-"+suffix)
		var cert adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/certificates", map[string]any{
			"ca_certificate_pem": certPEM,
		}, defaultTestKey, tunnelsBetaHeader), &cert)
		if cert.Type != "tunnel_certificate" || cert.Fingerprint == "" {
			t.Fatalf("certificate = %+v", cert)
		}

		var second adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/certificates", map[string]any{
			"ca_certificate_pem": certPEM2,
		}, defaultTestKey, tunnelsBetaHeader), &second)
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/certificates", map[string]any{
			"ca_certificate_pem": certPEM3,
		}, defaultTestKey, tunnelsBetaHeader)
		assertError(t, resp, http.StatusConflict, "conflict_error")

		var archived adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/archive", nil, defaultTestKey, tunnelsBetaHeader), &archived)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived tunnel = %+v, want archived_at", archived)
		}

		var certs adminTokenPage
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/tunnels/"+tunnelID+"/certificates?include_archived=true", nil, defaultTestKey, tunnelsBetaHeader), &certs)
		for _, archivedCert := range certs.Data {
			if archivedCert.ArchivedAt == nil {
				t.Fatalf("certificate after tunnel archive = %+v, want archived_at", archivedCert)
			}
		}
	})

	t.Run("success admin tables have no foreign keys", func(t *testing.T) {
		tables := []string{"users", "organization_invites", "workspace_members", "external_keys", "mcp_tunnels", "mcp_tunnel_certificates", "workspaces", "api_keys"}
		var foreignKeyCount int
		if err := app.db.Pool.QueryRow(context.Background(), `
			select count(*)
			from information_schema.table_constraints
			where constraint_type = 'FOREIGN KEY'
				and table_schema = 'public'
				and table_name = any($1)
		`, tables).Scan(&foreignKeyCount); err != nil {
			t.Fatalf("count admin foreign keys: %v", err)
		}
		if foreignKeyCount != 0 {
			t.Fatalf("admin foreign key count = %d, want 0", foreignKeyCount)
		}
	})
}

func adminDo(t *testing.T, app *testApp, method, path string, body any, key, beta string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, app.baseURL+path, reader)
	if err != nil {
		t.Fatalf("new admin request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do admin request: %v", err)
	}
	return resp
}

func adminDecodeOK(t *testing.T, resp *http.Response, target any) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	decodeJSON(t, resp.Body, target)
}

func createAdminInvite(t *testing.T, app *testApp, email, role string) adminObject {
	t.Helper()
	var invite adminObject
	adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/invites", map[string]any{
		"email": email,
		"role":  role,
	}, defaultTestKey, ""), &invite)
	if invite.Type != "invite" || invite.ID == "" {
		t.Fatalf("invite = %+v", invite)
	}
	return invite
}

func createAdminWorkspace(t *testing.T, app *testApp, name string, externalKeyID *string, tags map[string]string) adminObject {
	t.Helper()
	body := map[string]any{"name": name}
	if externalKeyID != nil {
		body["external_key_id"] = *externalKeyID
	}
	if tags != nil {
		body["tags"] = tags
	}
	var workspace adminObject
	adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/workspaces", body, defaultTestKey, ""), &workspace)
	if workspace.Type != "workspace" || workspace.ID == "" {
		t.Fatalf("workspace = %+v", workspace)
	}
	return workspace
}

func createAdminExternalKey(t *testing.T, app *testApp, name string) adminObject {
	t.Helper()
	var key adminObject
	adminDecodeOK(t, adminDo(t, app, http.MethodPost, "/v1/organizations/external_keys", map[string]any{
		"display_name": name,
		"geo":          "us",
		"provider_config": map[string]any{
			"type":     "aws",
			"kms_arn":  "arn:aws:kms:us-east-1:123456789012:key/" + name,
			"role_arn": "arn:aws:iam::123456789012:role/demo",
		},
	}, defaultTestKey, ""), &key)
	if key.Type != "external_key" || key.ID == "" {
		t.Fatalf("external key = %+v", key)
	}
	return key
}

func forceInviteTimes(t *testing.T, database *db.DB, olderID, newerID string) {
	t.Helper()
	base := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	if _, err := database.Pool.Exec(context.Background(), `
		update organization_invites
		set invited_at = case external_id
			when $1 then $3::timestamptz
			when $2 then $4::timestamptz
			else invited_at
		end
		where external_id in ($1, $2)
	`, olderID, newerID, base, base.Add(time.Second)); err != nil {
		t.Fatalf("force invite times: %v", err)
	}
}

func forceAPIKeyTimes(t *testing.T, database *db.DB, olderID, newerID string) {
	t.Helper()
	base := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	if _, err := database.Pool.Exec(context.Background(), `
		update api_keys
		set created_at = case external_id
			when $1 then $3::timestamptz
			when $2 then $4::timestamptz
			else created_at
		end,
		updated_at = case external_id
			when $1 then $3::timestamptz
			when $2 then $4::timestamptz
			else updated_at
		end
		where external_id in ($1, $2)
	`, olderID, newerID, base, base.Add(time.Second)); err != nil {
		t.Fatalf("force api key times: %v", err)
	}
}

func seedAdminUser(t *testing.T, database *db.DB, email, role string) string {
	t.Helper()
	ids := getAdminDefaultIDs(t, database)
	userID := "user_admin_" + uniqueAdminSuffix()
	if _, err := database.Pool.Exec(context.Background(), `
		insert into users (external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, $5)
	`, userID, ids.OrganizationID, email, "Admin Test User", role); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	return userID
}

func seedAdminAPIKey(t *testing.T, database *db.DB, suffix, rawKey string) (string, string) {
	t.Helper()
	ids := getAdminDefaultIDs(t, database)
	apiKeyID := "api_key_admin_" + suffix
	if _, err := database.Pool.Exec(context.Background(), `
		insert into api_keys (external_id, workspace_id, key_hash, status, created_by_user_id, name, partial_key_hint)
		values ($1, $2, $3, 'active', $4, $5, $6)
	`, apiKeyID, ids.WorkspaceID, auth.HashAPIKey(rawKey), ids.UserID, "Admin status test", partialTestKeyHint(rawKey)); err != nil {
		t.Fatalf("seed admin api key: %v", err)
	}
	return apiKeyID, rawKey
}

func seedAdminTunnel(t *testing.T, database *db.DB, tunnelID, domain string, workspaceExternalID *string) string {
	t.Helper()
	ids := getAdminDefaultIDs(t, database)
	var workspaceID *int64
	var workspaceIDText *string
	if workspaceExternalID != nil {
		var loadedWorkspaceID int64
		if err := database.Pool.QueryRow(context.Background(), `
			select id
			from workspaces
			where external_id = $1 and organization_id = $2
		`, *workspaceExternalID, ids.OrganizationID).Scan(&loadedWorkspaceID); err != nil {
			t.Fatalf("load tunnel workspace: %v", err)
		}
		workspaceID = &loadedWorkspaceID
		workspaceIDText = workspaceExternalID
	}
	displayName := "Tunnel " + tunnelID
	if _, err := database.Pool.Exec(context.Background(), `
		insert into mcp_tunnels (
			external_id, organization_id, workspace_id, workspace_external_id, display_name, domain
		)
		values ($1, $2, $3, $4, $5, $6)
	`, tunnelID, ids.OrganizationID, workspaceID, workspaceIDText, displayName, domain); err != nil {
		t.Fatalf("seed admin tunnel: %v", err)
	}
	return tunnelID
}

func getAdminDefaultIDs(t *testing.T, database *db.DB) adminDefaultIDs {
	t.Helper()
	var ids adminDefaultIDs
	if err := database.Pool.QueryRow(context.Background(), `
		select o.id, w.id, u.id
		from organizations o
		join workspaces w on w.organization_id = o.id and w.external_id = 'workspace_default'
		join users u on u.organization_id = o.id and u.external_id = 'user_default'
		where o.external_id = 'org_default'
	`).Scan(&ids.OrganizationID, &ids.WorkspaceID, &ids.UserID); err != nil {
		t.Fatalf("load admin default ids: %v", err)
	}
	return ids
}

func newTestCertificatePEM(t *testing.T, commonName string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate cert key: %v", err)
	}
	now := time.Now().UTC()
	template := x509.Certificate{
		SerialNumber:          big.NewInt(now.UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func containsAdminObject(items []adminObject, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func uniqueAdminSuffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func partialTestKeyHint(key string) string {
	if len(key) <= 12 {
		return key
	}
	return fmt.Sprintf("%s...%s", key[:8], key[len(key)-4:])
}
