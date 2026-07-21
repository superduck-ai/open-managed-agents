package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
)

func TestConsoleWorkspaceArchive(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-workspace-archive-bucket"))
	defer app.close()
	cookies := app.platformLoginCookies(t, "console-workspace-archive@example.com")

	var orgUUID string
	var orgID int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		select uuid::text, id from organizations where external_id = 'org_default'
	`).Scan(&orgUUID, &orgID); err != nil {
		t.Fatalf("load default organization: %v", err)
	}
	base := "/api/console/organizations/" + orgUUID

	t.Run("default workspace alias cannot be archived", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/default/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("default workspace cannot be archived by external_id", func(t *testing.T) {
		// The handler blocks the "default" alias; the DB-layer invariant must
		// also hold when the caller passes a default workspace's real
		// external_id, so the guard cannot be bypassed by knowing the id.
		// Exercised directly at the DB layer to avoid the HTTP session's
		// own-workspace guard, which the alias case already covers.
		var defaultWSExternalID string
		if err := app.db.Pool.QueryRow(context.Background(), `
			select external_id from workspaces where organization_id = $1 and name = 'default'
			limit 1
		`, orgID).Scan(&defaultWSExternalID); err != nil {
			t.Fatalf("load default workspace external_id: %v", err)
		}
		_, err := app.db.ArchiveConsoleWorkspace(context.Background(), orgUUID, defaultWSExternalID)
		if !errors.Is(err, platform.ErrNotFound) {
			t.Fatalf("ArchiveConsoleWorkspace err = %v, want ErrNotFound", err)
		}
		var archivedAt *time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from workspaces where external_id = $1
		`, defaultWSExternalID).Scan(&archivedAt); err != nil {
			t.Fatalf("reload default workspace archived_at: %v", err)
		}
		if archivedAt != nil {
			t.Fatalf("default workspace %q was archived despite the invariant", defaultWSExternalID)
		}
	})

	t.Run("unknown workspace is not found", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/ws_archive_missing/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("archive is isolated by organization", func(t *testing.T) {
		otherOrgID := seedArchiveOrganization(t, app, "org_archive_isolation_"+uniqueAdminSuffix())
		otherWS := seedArchiveTargetWorkspace(t, app, otherOrgID, "Other Org WS")
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+otherWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (org isolation): %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("cannot archive current workspace", func(t *testing.T) {
		myWS := seedArchiveTargetWorkspace(t, app, orgID, "My Current WS")
		var myWSID int64
		var myWSUUID string
		if err := app.db.Pool.QueryRow(context.Background(), `
			select id, uuid::text from workspaces where external_id = $1
		`, myWS).Scan(&myWSID, &myWSUUID); err != nil {
			t.Fatalf("load new workspace: %v", err)
		}

		sessionCookie := responseCookie(cookies, "sessionKey")
		if sessionCookie == nil {
			t.Fatalf("sessionKey cookie not found in %#v", cookies)
		}
		sessionKey := sessionCookie.Value
		session, err := app.sessions.Get(context.Background(), sessionKey)
		if err != nil {
			t.Fatalf("get session: %v", err)
		}
		origWSID, origWSUUID, origWSExternal := session.WorkspaceID, session.WorkspaceUUID, session.WorkspaceExternalID
		session.WorkspaceID, session.WorkspaceUUID, session.WorkspaceExternalID = myWSID, myWSUUID, myWS
		if err := app.sessions.Save(context.Background(), sessionKey, session); err != nil {
			t.Fatalf("bind session to current workspace: %v", err)
		}
		defer func() {
			session.WorkspaceID, session.WorkspaceUUID, session.WorkspaceExternalID = origWSID, origWSUUID, origWSExternal
			if err := app.sessions.Save(context.Background(), sessionKey, session); err != nil {
				t.Fatalf("restore session workspace: %v", err)
			}
		}()

		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+myWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		if body["error"] != "cannot_archive_current_workspace" {
			t.Fatalf("error = %v, want cannot_archive_current_workspace", body["error"])
		}
	})

	targetWS := seedArchiveTargetWorkspace(t, app, orgID, "Archive Target")
	keyID := seedConsoleAPIKeyForWorkspace(t, app, orgUUID, targetWS, "target key")
	liveKeyHash := seedLiveAPIKeyForWorkspace(t, app, targetWS)

	t.Run("archive succeeds and cascades to api keys", func(t *testing.T) {
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+targetWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var archived map[string]any
		decodeJSON(t, resp.Body, &archived)
		if archived["id"] != targetWS || archived["archived_at"] == nil {
			t.Fatalf("archive response mismatch: %#v", archived)
		}
		var keyArchivedAt *time.Time
		var keyStatus string
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at, status from console_api_keys where external_id = $1
		`, keyID).Scan(&keyArchivedAt, &keyStatus); err != nil {
			t.Fatalf("load cascaded console api key: %v", err)
		}
		if keyArchivedAt == nil {
			t.Fatalf("console api key %q was not cascaded to archived", keyID)
		}
		if keyStatus != "archived" {
			t.Fatalf("console api key %q status = %q, want archived", keyID, keyStatus)
		}
		// The workspace's live API key (the one /v1 auth actually checks) must
		// also be revoked, otherwise callers can keep authenticating against an
		// archived workspace.
		if _, err := app.db.GetAPIKey(context.Background(), liveKeyHash); !errors.Is(err, platform.ErrNotFound) {
			t.Fatalf("GetAPIKey err = %v, want not-found for a revoked key of an archived workspace", err)
		}
	})

	t.Run("archive is idempotent", func(t *testing.T) {
		var firstArchivedAt time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from workspaces where external_id = $1
		`, targetWS).Scan(&firstArchivedAt); err != nil {
			t.Fatalf("load workspace archived_at: %v", err)
		}
		resp := app.doPlatformConsole(t, http.MethodPost, base+"/workspaces/"+targetWS+"/archive", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("repeat status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var secondArchivedAt time.Time
		if err := app.db.Pool.QueryRow(context.Background(), `
			select archived_at from workspaces where external_id = $1
		`, targetWS).Scan(&secondArchivedAt); err != nil {
			t.Fatalf("reload workspace archived_at: %v", err)
		}
		if !secondArchivedAt.Equal(firstArchivedAt) {
			t.Fatalf("archived_at changed on repeat archive: first=%s second=%s", firstArchivedAt, secondArchivedAt)
		}
	})
}

func seedArchiveOrganization(t *testing.T, app *testApp, externalID string) int64 {
	t.Helper()
	var id int64
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into organizations (external_id, name)
		values ($1, $1)
		on conflict (external_id) do update set name = excluded.name
		returning id
	`, externalID).Scan(&id); err != nil {
		t.Fatalf("seed archive organization: %v", err)
	}
	return id
}

func seedArchiveTargetWorkspace(t *testing.T, app *testApp, orgID int64, name string) string {
	t.Helper()
	suffix := uniqueAdminSuffix()
	externalID := "ws_archive_" + suffix
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, $3)
	`, externalID, orgID, name+" "+suffix); err != nil {
		t.Fatalf("seed archive target workspace: %v", err)
	}
	return externalID
}

func seedConsoleAPIKeyForWorkspace(t *testing.T, app *testApp, orgUUID, workspaceExternalID, name string) string {
	t.Helper()
	externalID := "cak_archive_" + uniqueAdminSuffix()
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into console_api_keys (external_id, api_key_uuid, org_uuid, workspace_id, name, key_prefix, key_suffix, key_hash, status)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
	`, externalID, "akuuid_"+externalID, orgUUID, workspaceExternalID, name, "sk-ant-", "ARCH",
		auth.HashAPIKey("secret-"+externalID)); err != nil {
		t.Fatalf("seed console api key for workspace: %v", err)
	}
	return externalID
}

// seedLiveAPIKeyForWorkspace seeds a row in api_keys, the table /v1 request
// auth (DB.GetAPIKey) actually checks — distinct from console_api_keys, which
// only backs the console management UI. It returns the key's hash so the
// caller can assert on GetAPIKey behavior after archiving the workspace.
func seedLiveAPIKeyForWorkspace(t *testing.T, app *testApp, workspaceExternalID string) string {
	t.Helper()
	externalID := "ak_archive_" + uniqueAdminSuffix()
	keyHash := auth.HashAPIKey("secret-" + externalID)
	tag, err := app.db.Pool.Exec(context.Background(), `
		insert into api_keys (external_id, workspace_id, key_hash, status)
		select $1, w.id, $2, 'active'
		  from workspaces w
		 where w.external_id = $3
	`, externalID, keyHash, workspaceExternalID)
	if err != nil {
		t.Fatalf("seed live api key for workspace: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("seed live api key for workspace %q: expected 1 row inserted, got %d", workspaceExternalID, tag.RowsAffected())
	}
	return keyHash
}
