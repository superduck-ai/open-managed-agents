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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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
	OrganizationUUID string
	WorkspaceUUID    string
	UserUUID         string
}

func TestAdminResourceReferencesUseUUID(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()

	expectedUUIDColumns := map[string][]string{
		"users":                {"organization_uuid"},
		"organization_invites": {"organization_uuid"},
		"api_keys":             {"workspace_uuid", "created_by_user_uuid"},
		"workspace_members":    {"organization_uuid", "workspace_uuid", "user_uuid"},
		"external_keys":        {"organization_uuid"},
	}
	for table, columns := range expectedUUIDColumns {
		for _, column := range columns {
			var dataType string
			if err := app.pool.QueryRow(context.Background(), `
				select data_type
				from information_schema.columns
				where table_schema = current_schema()
					and table_name = $1
					and column_name = $2
			`, table, column).Scan(&dataType); err != nil {
				t.Fatalf("load %s.%s type: %v", table, column, err)
			}
			if dataType != "uuid" {
				t.Fatalf("%s.%s type = %q, want uuid", table, column, dataType)
			}
		}
	}

	legacyColumns := map[string][]string{
		"users":                {"organization_id"},
		"organization_invites": {"organization_id"},
		"api_keys":             {"workspace_id", "created_by_user_id"},
		"workspace_members":    {"organization_id", "workspace_id", "user_id"},
		"external_keys":        {"organization_id"},
	}
	for table, columns := range legacyColumns {
		for _, column := range columns {
			var count int
			if err := app.pool.QueryRow(context.Background(), `
				select count(*)
				from information_schema.columns
				where table_schema = current_schema()
					and table_name = $1
					and column_name = $2
			`, table, column).Scan(&count); err != nil {
				t.Fatalf("check legacy column %s.%s: %v", table, column, err)
			}
			if count != 0 {
				t.Fatalf("legacy column %s.%s still exists", table, column)
			}
		}
	}

	var externalKeyIndexDefinition string
	if err := app.pool.QueryRow(context.Background(), `
		select indexdef
		from pg_indexes
		where schemaname = current_schema()
			and tablename = 'external_keys'
			and indexname = 'external_keys_organization_created_v1_idx'
	`).Scan(&externalKeyIndexDefinition); err != nil {
		t.Fatalf("load external key pagination index: %v", err)
	}
	if !strings.Contains(externalKeyIndexDefinition, "created_at DESC, uuid DESC") ||
		strings.Contains(externalKeyIndexDefinition, "created_at DESC, id DESC") {
		t.Fatalf("external key pagination index = %q, want UUID tie-breaker", externalKeyIndexDefinition)
	}
}

func TestWorkspaceOrganizationReferenceUsesUUID(t *testing.T) {
	app := newTestApp(t, nil)
	defer app.close()

	var legacyColumnCount int
	if err := app.pool.QueryRow(context.Background(), `
		select count(*)
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'workspaces'
			and column_name = 'organization_id'
	`).Scan(&legacyColumnCount); err != nil {
		t.Fatalf("query legacy workspace organization column: %v", err)
	}
	if legacyColumnCount != 0 {
		t.Fatalf("workspace organization_id column count = %d, want 0", legacyColumnCount)
	}
	if err := app.pool.QueryRow(context.Background(), `
		select count(*)
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'organizations'
			and column_name = 'external_id'
	`).Scan(&legacyColumnCount); err != nil {
		t.Fatalf("query legacy organization external ID column: %v", err)
	}
	if legacyColumnCount != 0 {
		t.Fatalf("organization external_id column count = %d, want 0", legacyColumnCount)
	}
	var dataType string
	var ordinalPosition int
	if err := app.pool.QueryRow(context.Background(), `
		select data_type, ordinal_position
		from information_schema.columns
		where table_schema = current_schema()
			and table_name = 'workspaces'
			and column_name = 'organization_uuid'
	`).Scan(&dataType, &ordinalPosition); err != nil {
		t.Fatalf("query workspace organization UUID column: %v", err)
	}
	if dataType != "uuid" || ordinalPosition != 4 {
		t.Fatalf("workspace organization_uuid = type %s ordinal %d, want uuid at 4", dataType, ordinalPosition)
	}

	var referenceMatches bool
	if err := app.pool.QueryRow(context.Background(), `
		select w.organization_uuid = o.uuid
		from workspaces w
		join organizations o on o.uuid = w.organization_uuid
		where w.external_id = 'workspace_default'
	`).Scan(&referenceMatches); err != nil {
		t.Fatalf("query default workspace organization UUID: %v", err)
	}
	if !referenceMatches {
		t.Fatal("default workspace organization_uuid does not match organization uuid")
	}

	var originalOrganizationUUID string
	var organizationCount int
	if err := app.pool.QueryRow(context.Background(), `
		select o.uuid::text, (select count(*) from organizations)
		from organizations o
		join workspaces w on w.organization_uuid = o.uuid
		where w.external_id = 'workspace_default'
	`).Scan(&originalOrganizationUUID, &organizationCount); err != nil {
		t.Fatalf("load organization state before repeated seed: %v", err)
	}
	if err := app.db.Seed(context.Background(), app.cfg.Bootstrap.SeedAPIKeys); err != nil {
		t.Fatalf("repeat default seed: %v", err)
	}
	var seededOrganizationUUID string
	var seededOrganizationCount int
	if err := app.pool.QueryRow(context.Background(), `
		select o.uuid::text, (select count(*) from organizations)
		from organizations o
		join workspaces w on w.organization_uuid = o.uuid
		where w.external_id = 'workspace_default'
	`).Scan(&seededOrganizationUUID, &seededOrganizationCount); err != nil {
		t.Fatalf("load organization state after repeated seed: %v", err)
	}
	if seededOrganizationUUID != originalOrganizationUUID || seededOrganizationCount != organizationCount {
		t.Fatalf(
			"repeated seed changed organization state: UUID %s -> %s, count %d -> %d",
			originalOrganizationUUID,
			seededOrganizationUUID,
			organizationCount,
			seededOrganizationCount,
		)
	}
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

	t.Run("failure update missing api key", func(t *testing.T) {
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/api_keys/api_key_missing_"+suffix, map[string]any{
			"name": "missing-" + suffix,
		}, defaultTestKey, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
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

	t.Run("failure api key list rejects conflicting cursors", func(t *testing.T) {
		resp := adminDo(
			t,
			app,
			http.MethodGet,
			"/v1/organizations/api_keys?after_id=api_key_after&before_id=api_key_before",
			nil,
			defaultTestKey,
			"",
		)
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
		tunnelID := seedAdminTunnel(t, app.pool, "tunnel_bad_cert_"+suffix, "bad-cert-"+suffix+".local", nil)
		resp := adminDo(t, app, http.MethodPost, "/v1/organizations/tunnels/"+tunnelID+"/certificates", map[string]any{
			"ca_certificate_pem": "not a certificate",
		}, defaultTestKey, tunnelsBetaHeader)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure cross organization isolation", func(t *testing.T) {
		otherKey := "sk-ant-admin-other-" + suffix
		seedWorkspaceKey(t, app.pool, "org_admin_other_"+suffix, "workspace_admin_other_"+suffix, "api_key_admin_other_"+suffix, otherKey)
		resp := adminDo(t, app, http.MethodGet, "/v1/organizations/workspaces/workspace_default", nil, otherKey, "")
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("success organization me", func(t *testing.T) {
		apiKey, err := app.db.GetAPIKey(context.Background(), auth.HashAPIKey(defaultTestKey))
		if err != nil {
			t.Fatalf("load default API key: %v", err)
		}
		var org adminObject
		adminDecodeOK(t, adminDo(t, app, http.MethodGet, "/v1/organizations/me", nil, defaultTestKey, ""), &org)
		if org.ID != apiKey.OrganizationUUID.String() || org.Type != "organization" {
			t.Fatalf("organization = %+v, want UUID %s organization", org, apiKey.OrganizationUUID)
		}
	})

	t.Run("success invites paginate and soft delete", func(t *testing.T) {
		first := createAdminInvite(t, app, "one-"+suffix+"@example.com", "user")
		second := createAdminInvite(t, app, "two-"+suffix+"@example.com", "developer")
		forceInviteTimes(t, app.pool, first.ID, second.ID)

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
		userID := seedAdminUser(t, app.pool, "member-"+suffix+"@example.com", "developer")

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
		apiKeyID, rawKey := seedAdminAPIKey(t, app.pool, "status-"+suffix, "sk-ant-admin-status-"+suffix)
		pageKeyID, _ := seedAdminAPIKey(t, app.pool, "page-"+suffix, "sk-ant-admin-page-"+suffix)
		forceAPIKeyTimes(t, app.pool, apiKeyID, pageKeyID)

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

	t.Run("success api key before cursor returns nearest previous page", func(t *testing.T) {
		creatorID := seedAdminUser(t, app.pool, "before-key-creator-"+suffix+"@example.com", "developer")
		oldestID, _ := seedAdminAPIKey(t, app.pool, "before-oldest-"+suffix, "sk-ant-admin-before-oldest-"+suffix)
		olderMiddleID, _ := seedAdminAPIKey(t, app.pool, "before-older-middle-"+suffix, "sk-ant-admin-before-older-middle-"+suffix)
		newerMiddleID, _ := seedAdminAPIKey(t, app.pool, "before-newer-middle-"+suffix, "sk-ant-admin-before-newer-middle-"+suffix)
		newestID, _ := seedAdminAPIKey(t, app.pool, "before-newest-"+suffix, "sk-ant-admin-before-newest-"+suffix)
		if _, err := app.pool.Exec(context.Background(), `
			update api_keys ak
			set created_by_user_uuid = u.uuid
			from users u
			where u.external_id = $1
				and ak.external_id in ($2, $3, $4, $5)
		`, creatorID, oldestID, olderMiddleID, newerMiddleID, newestID); err != nil {
			t.Fatalf("assign API key creator: %v", err)
		}
		forceAPIKeyTimes(t, app.pool, oldestID, olderMiddleID, newerMiddleID, newestID)

		var middlePage adminCursorPage
		adminDecodeOK(t, adminDo(
			t,
			app,
			http.MethodGet,
			"/v1/organizations/api_keys?limit=2&created_by_user_id="+creatorID+"&before_id="+oldestID,
			nil,
			defaultTestKey,
			"",
		), &middlePage)
		if len(middlePage.Data) != 2 ||
			middlePage.Data[0].ID != newerMiddleID ||
			middlePage.Data[1].ID != olderMiddleID ||
			!middlePage.HasMore ||
			middlePage.FirstID == nil {
			t.Fatalf("middle before page = %+v, want nearest keys %s and %s", middlePage, newerMiddleID, olderMiddleID)
		}

		var newestPage adminCursorPage
		adminDecodeOK(t, adminDo(
			t,
			app,
			http.MethodGet,
			"/v1/organizations/api_keys?limit=2&created_by_user_id="+creatorID+"&before_id="+*middlePage.FirstID,
			nil,
			defaultTestKey,
			"",
		), &newestPage)
		if len(newestPage.Data) != 1 || newestPage.Data[0].ID != newestID || newestPage.HasMore {
			t.Fatalf("newest before page = %+v, want key %s", newestPage, newestID)
		}
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
		tunnelID := seedAdminTunnel(t, app.pool, "tunnel_"+suffix, "tunnel-"+suffix+".local", &workspace.ID)

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
		if err := app.pool.QueryRow(context.Background(), `
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

	t.Run("success tunnel references use UUID columns", func(t *testing.T) {
		var uuidColumnCount, legacyColumnCount int
		if err := app.pool.QueryRow(context.Background(), `
			select
				count(*) filter (
					where data_type = 'uuid'
						and (
							(table_name = 'mcp_tunnels' and column_name in ('organization_uuid', 'workspace_uuid'))
							or (table_name = 'mcp_tunnel_certificates' and column_name in ('organization_uuid', 'tunnel_uuid'))
						)
				),
				count(*) filter (
					where column_name in ('organization_id', 'workspace_id', 'tunnel_id')
				)
			from information_schema.columns
			where table_schema = current_schema()
				and table_name in ('mcp_tunnels', 'mcp_tunnel_certificates')
		`).Scan(&uuidColumnCount, &legacyColumnCount); err != nil {
			t.Fatalf("inspect tunnel reference columns: %v", err)
		}
		if uuidColumnCount != 4 || legacyColumnCount != 0 {
			t.Fatalf("tunnel reference columns = %d UUID and %d legacy, want 4 and 0", uuidColumnCount, legacyColumnCount)
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

func forceInviteTimes(t *testing.T, pool *pgxpool.Pool, olderID, newerID string) {
	t.Helper()
	base := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	if _, err := pool.Exec(context.Background(), `
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

func forceAPIKeyTimes(t *testing.T, pool *pgxpool.Pool, apiKeyIDs ...string) {
	t.Helper()
	base := time.Now().UTC().Add(100 * 365 * 24 * time.Hour)
	for index, apiKeyID := range apiKeyIDs {
		createdAt := base.Add(time.Duration(index) * time.Second)
		if _, err := pool.Exec(context.Background(), `
			update api_keys
			set created_at = $2::timestamptz,
				updated_at = $2::timestamptz
			where external_id = $1
		`, apiKeyID, createdAt); err != nil {
			t.Fatalf("force api key %q time: %v", apiKeyID, err)
		}
	}
}

func seedAdminUser(t *testing.T, pool *pgxpool.Pool, email, role string) string {
	t.Helper()
	ids := getAdminDefaultIDs(t, pool)
	userID := "user_admin_" + uniqueAdminSuffix()
	if _, err := pool.Exec(context.Background(), `
		insert into users (external_id, organization_uuid, email, name, role)
		values ($1, $2, $3, $4, $5)
	`, userID, ids.OrganizationUUID, email, "Admin Test User", role); err != nil {
		t.Fatalf("seed admin user: %v", err)
	}
	return userID
}

func seedAdminAPIKey(t *testing.T, pool *pgxpool.Pool, suffix, rawKey string) (string, string) {
	t.Helper()
	ids := getAdminDefaultIDs(t, pool)
	apiKeyID := "api_key_admin_" + suffix
	if _, err := pool.Exec(context.Background(), `
		insert into api_keys (
			external_id, workspace_uuid, key_hash, status, created_by_user_uuid, name, partial_key_hint
		)
		values ($1, $2, $3, 'active', $4, $5, $6)
	`, apiKeyID, ids.WorkspaceUUID, auth.HashAPIKey(rawKey), ids.UserUUID, "Admin status test", partialTestKeyHint(rawKey)); err != nil {
		t.Fatalf("seed admin api key: %v", err)
	}
	return apiKeyID, rawKey
}

func seedAdminTunnel(t *testing.T, pool *pgxpool.Pool, tunnelID, domain string, workspaceExternalID *string) string {
	t.Helper()
	ids := getAdminDefaultIDs(t, pool)
	var workspaceUUID *string
	if workspaceExternalID != nil {
		var loadedWorkspaceUUID string
		if err := pool.QueryRow(context.Background(), `
			select CAST(uuid AS text)
			from workspaces
			where external_id = $1
				and organization_uuid = CAST($2 AS uuid)
		`, *workspaceExternalID, ids.OrganizationUUID).Scan(&loadedWorkspaceUUID); err != nil {
			t.Fatalf("load tunnel workspace: %v", err)
		}
		workspaceUUID = &loadedWorkspaceUUID
	}
	displayName := "Tunnel " + tunnelID
	if _, err := pool.Exec(context.Background(), `
		insert into mcp_tunnels (
			external_id, organization_uuid, workspace_uuid, workspace_external_id, display_name, domain
		)
		values ($1, $2, $3, $4, $5, $6)
	`, tunnelID, ids.OrganizationUUID, workspaceUUID, workspaceExternalID, displayName, domain); err != nil {
		t.Fatalf("seed admin tunnel: %v", err)
	}
	return tunnelID
}

func getAdminDefaultIDs(t *testing.T, pool *pgxpool.Pool) adminDefaultIDs {
	t.Helper()
	var ids adminDefaultIDs
	if err := pool.QueryRow(context.Background(), `
		select CAST(w.organization_uuid AS text), CAST(w.uuid AS text), CAST(u.uuid AS text)
		from workspaces w
		join users u on u.organization_uuid = w.organization_uuid and u.external_id = 'user_default'
		where w.external_id = 'workspace_default'
	`).Scan(&ids.OrganizationUUID, &ids.WorkspaceUUID, &ids.UserUUID); err != nil {
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
