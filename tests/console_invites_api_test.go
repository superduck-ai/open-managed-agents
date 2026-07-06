package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestConsoleInvitesAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-invites-bucket"))
	defer app.close()
	cookies := app.platformLoginCookies(t, "console-invites@example.com")

	var orgUUID string
	var orgID int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select o.uuid::text, o.id
		from organizations o
		where o.external_id = 'org_default'
	`).Scan(&orgUUID, &orgID); err != nil {
		t.Fatalf("load default organization ids: %v", err)
	}
	path := "/api/console/organizations/" + orgUUID + "/invites"

	t.Run("failure rejects invalid create payload", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"email":"not-an-email","role":"billing"}`), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid email status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		resp = app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"email":"valid@example.com","role":"owner"}`), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid role status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("failure rejects invalid list status", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, path+"?status=unknown", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid status response = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("failure rejects unknown invite actions", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodPut, path+"/invite_missing", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown invite resend status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}

		resp = app.platformRequest(t, http.MethodDelete, path+"/invite_missing", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown invite delete status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success recovers mirrored official organization uuid", func(t *testing.T) {
		aliasCookies := app.platformLoginCookies(t, "console-invites-alias@example.com")
		sessionCookie := responseCookie(aliasCookies, "sessionKey")
		if sessionCookie == nil {
			t.Fatalf("platform login cookies = %#v, want sessionKey", aliasCookies)
		}
		if err := app.sessions.Delete(context.Background(), sessionCookie.Value); err != nil {
			t.Fatalf("delete platform session: %v", err)
		}
		officialOrgPath := "/api/console/organizations/7294b4e5-c50b-48d9-bef8-c7a19423262c/invites"
		email := "mirrored-org-" + uniqueAdminSuffix() + "@example.com"
		resp := app.platformRequest(t, http.MethodPost, officialOrgPath, strings.NewReader(`{"email":"`+email+`","role":"billing"}`), aliasCookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mirrored official org create status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var created map[string]any
		decodeJSON(t, resp.Body, &created)
		if created["email"] != email || created["role"] != "billing" || created["status"] != "pending" {
			t.Fatalf("mirrored official org invite = %#v", created)
		}
		createdID, _ := created["id"].(string)
		resendResp := app.platformRequest(t, http.MethodPut, officialOrgPath+"/"+createdID, nil, aliasCookies)
		defer resendResp.Body.Close()
		if resendResp.StatusCode != http.StatusOK {
			t.Fatalf("mirrored official org resend status = %d, want 200: %s", resendResp.StatusCode, readAll(t, resendResp.Body))
		}
		var resent map[string]any
		decodeJSON(t, resendResp.Body, &resent)
		if resent["id"] != createdID || resent["email"] != email || resent["status"] != "pending" {
			t.Fatalf("mirrored official org resent invite = %#v", resent)
		}

		deleteResp := app.platformRequest(t, http.MethodDelete, officialOrgPath+"/"+createdID, nil, aliasCookies)
		defer deleteResp.Body.Close()
		if deleteResp.StatusCode != http.StatusOK {
			t.Fatalf("mirrored official org delete status = %d, want 200: %s", deleteResp.StatusCode, readAll(t, deleteResp.Body))
		}
		var deleted map[string]any
		decodeJSON(t, deleteResp.Body, &deleted)
		if deleted["id"] != createdID || deleted["type"] != "invite_deleted" {
			t.Fatalf("mirrored official org deleted invite = %#v", deleted)
		}
	})

	t.Run("success create and list pending invites", func(t *testing.T) {
		suffix := uniqueAdminSuffix()
		expiredID := "invite_expired_" + suffix
		if _, err := app.db.Pool.Exec(context.Background(), `
			insert into organization_invites (
				external_id, organization_id, email, role, status, invited_at, expires_at
			)
			values ($1, $2, $3, 'developer', 'pending', now() - interval '24 hours', now() - interval '1 hour')
		`, expiredID, orgID, "expired-"+suffix+"@example.com"); err != nil {
			t.Fatalf("seed expired invite: %v", err)
		}

		createResp := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"email":" Mixed+Case@Example.com ","role":"billing"}`), cookies)
		defer createResp.Body.Close()
		if createResp.StatusCode != http.StatusOK {
			t.Fatalf("create status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
		}
		var created map[string]any
		decodeJSON(t, createResp.Body, &created)
		createdID, _ := created["id"].(string)
		if !strings.HasPrefix(createdID, "invite_") ||
			created["type"] != "invite" ||
			created["email"] != "mixed+case@example.com" ||
			created["role"] != "billing" ||
			created["status"] != "pending" {
			t.Fatalf("created invite mismatch: %#v", created)
		}
		invitedAt, err := time.Parse(time.RFC3339Nano, created["invited_at"].(string))
		if err != nil {
			t.Fatalf("parse invited_at: %v", err)
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, created["expires_at"].(string))
		if err != nil {
			t.Fatalf("parse expires_at: %v", err)
		}
		if expiresAt.Sub(invitedAt) < 20*24*time.Hour || expiresAt.Sub(invitedAt) > 22*24*time.Hour {
			t.Fatalf("invite expiration = %s after invite, want about 21 days", expiresAt.Sub(invitedAt))
		}

		pendingResp := app.platformRequest(t, http.MethodGet, path+"?status=pending", nil, cookies)
		defer pendingResp.Body.Close()
		if pendingResp.StatusCode != http.StatusOK {
			t.Fatalf("pending list status = %d, want 200: %s", pendingResp.StatusCode, readAll(t, pendingResp.Body))
		}
		var pending []map[string]any
		decodeJSON(t, pendingResp.Body, &pending)
		if invite := findConsoleInvite(pending, createdID); invite == nil {
			t.Fatalf("created invite %q not found in pending list %#v", createdID, pending)
		}
		if invite := findConsoleInvite(pending, expiredID); invite != nil {
			t.Fatalf("expired invite appears in pending list: %#v", invite)
		}

		resendResp := app.platformRequest(t, http.MethodPut, path+"/"+createdID, nil, cookies)
		defer resendResp.Body.Close()
		if resendResp.StatusCode != http.StatusOK {
			t.Fatalf("resend status = %d, want 200: %s", resendResp.StatusCode, readAll(t, resendResp.Body))
		}
		var resent map[string]any
		decodeJSON(t, resendResp.Body, &resent)
		if resent["id"] != createdID ||
			resent["type"] != "invite" ||
			resent["email"] != "mixed+case@example.com" ||
			resent["role"] != "billing" ||
			resent["status"] != "pending" {
			t.Fatalf("resent invite mismatch: %#v", resent)
		}
		resentInvitedAt, err := time.Parse(time.RFC3339Nano, resent["invited_at"].(string))
		if err != nil {
			t.Fatalf("parse resent invited_at: %v", err)
		}
		resentExpiresAt, err := time.Parse(time.RFC3339Nano, resent["expires_at"].(string))
		if err != nil {
			t.Fatalf("parse resent expires_at: %v", err)
		}
		if resentExpiresAt.Sub(resentInvitedAt) < 20*24*time.Hour || resentExpiresAt.Sub(resentInvitedAt) > 22*24*time.Hour {
			t.Fatalf("resent invite expiration = %s after invite, want about 21 days", resentExpiresAt.Sub(resentInvitedAt))
		}

		deleteResp := app.platformRequest(t, http.MethodDelete, path+"/"+createdID, nil, cookies)
		defer deleteResp.Body.Close()
		if deleteResp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", deleteResp.StatusCode, readAll(t, deleteResp.Body))
		}
		var deleted map[string]any
		decodeJSON(t, deleteResp.Body, &deleted)
		if deleted["id"] != createdID || deleted["type"] != "invite_deleted" {
			t.Fatalf("deleted invite = %#v, want id %s type invite_deleted", deleted, createdID)
		}

		pendingAfterDeleteResp := app.platformRequest(t, http.MethodGet, path+"?status=pending", nil, cookies)
		defer pendingAfterDeleteResp.Body.Close()
		if pendingAfterDeleteResp.StatusCode != http.StatusOK {
			t.Fatalf("pending after delete status = %d, want 200: %s", pendingAfterDeleteResp.StatusCode, readAll(t, pendingAfterDeleteResp.Body))
		}
		var pendingAfterDelete []map[string]any
		decodeJSON(t, pendingAfterDeleteResp.Body, &pendingAfterDelete)
		if invite := findConsoleInvite(pendingAfterDelete, createdID); invite != nil {
			t.Fatalf("deleted invite appears in pending list: %#v", invite)
		}

		deletedResp := app.platformRequest(t, http.MethodGet, path+"?status=deleted", nil, cookies)
		defer deletedResp.Body.Close()
		if deletedResp.StatusCode != http.StatusOK {
			t.Fatalf("deleted list status = %d, want 200: %s", deletedResp.StatusCode, readAll(t, deletedResp.Body))
		}
		var deletedList []map[string]any
		decodeJSON(t, deletedResp.Body, &deletedList)
		deletedInvite := findConsoleInvite(deletedList, createdID)
		if deletedInvite == nil || deletedInvite["status"] != "deleted" {
			t.Fatalf("deleted invite = %#v in %#v, want status deleted", deletedInvite, deletedList)
		}

		expiredResp := app.platformRequest(t, http.MethodGet, path+"?status=expired", nil, cookies)
		defer expiredResp.Body.Close()
		if expiredResp.StatusCode != http.StatusOK {
			t.Fatalf("expired list status = %d, want 200: %s", expiredResp.StatusCode, readAll(t, expiredResp.Body))
		}
		var expired []map[string]any
		decodeJSON(t, expiredResp.Body, &expired)
		expiredInvite := findConsoleInvite(expired, expiredID)
		if expiredInvite == nil || expiredInvite["status"] != "expired" {
			t.Fatalf("expired invite = %#v in %#v, want status expired", expiredInvite, expired)
		}
	})
}

func findConsoleInvite(invites []map[string]any, id string) map[string]any {
	for _, invite := range invites {
		if invite["id"] == id {
			return invite
		}
	}
	return nil
}
