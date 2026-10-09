package tests

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestWorkbenchAttachmentPermissions(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workbench-attachment"))
	t.Cleanup(app.close)
	refs := getAdminDefaultIDs(t, app.pool)
	forbiddenWorkspace := createAdminWorkspace(t, app, "upload-private-"+uniqueAdminSuffix(), nil, nil)
	for _, role := range []string{"user", "claude_code_user", "developer"} {
		t.Run(role, func(t *testing.T) {
			userID := seedAdminUser(t, app.pool, role+uniqueAdminSuffix()+"@example.local", role)
			cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
			list := app.platformRequest(t, http.MethodGet, "/api/organizations/"+refs.OrganizationUUID+"/workbench/prompts", nil, cookies)
			list.Body.Close()
			if list.StatusCode != http.StatusOK {
				t.Fatalf("Workbench list = %d", list.StatusCode)
			}
			body, contentType := multipartBody(t, "attachment.txt", "text/plain", []byte("Workbench attachment"), false)
			response := app.platformRequestWithHeaders(t, http.MethodPost, "/v1/files?beta=true", body, cookies, map[string]string{"Content-Type": contentType, "anthropic-beta": "files-api-2025-04-14"})
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Errorf("Workbench attachment = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
			}
			body, contentType = multipartBody(t, "attachment.txt", "text/plain", []byte("Workbench attachment"), false)
			denied := app.platformRequestWithHeaders(t, http.MethodPost, "/v1/files?beta=true", body, cookies, map[string]string{"Content-Type": contentType, "X-Workspace-ID": forbiddenWorkspace.ID})
			denied.Body.Close()
			if denied.StatusCode != http.StatusForbidden {
				t.Fatalf("nonmember attachment = %d", denied.StatusCode)
			}
			if role != "developer" {
				for _, path := range []string{"/v1/files?beta=true", "/v1/agents?beta=true"} {
					denied := app.platformRequest(t, http.MethodGet, path, nil, cookies)
					denied.Body.Close()
					if denied.StatusCode != http.StatusForbidden {
						t.Fatalf("development resource %s = %d", path, denied.StatusCode)
					}
				}
			}
		})
	}
}

func TestWorkbenchPromptWorkspacePermissions(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("workbench-workspace-scope"))
	t.Cleanup(app.close)
	refs := getAdminDefaultIDs(t, app.pool)
	created := createAdminWorkspace(t, app, "private-"+uniqueAdminSuffix(), nil, nil)
	workspace, err := app.db.GetAdminWorkspace(t.Context(), refs.OrganizationUUID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	promptID := uuid.NewV4().String()
	_, err = app.db.UpsertWorkbenchPrompt(t.Context(), platform.WorkbenchPromptRecord{OrgUUID: refs.OrganizationUUID, PromptUUID: promptID, WorkspaceUUID: workspace.UUID, WorkspaceDisplayID: workspace.ExternalID, Name: "private-workspace-prompt"})
	if err != nil {
		t.Fatal(err)
	}
	userID := seedAdminUser(t, app.pool, "outsider-"+uniqueAdminSuffix()+"@example.local", "user")
	cookies := workspaceUserCookies(t, app, refs.OrganizationUUID, userID)
	if _, _, err := workspaceaccess.New(app.db).Resolve(t.Context(), refs.OrganizationUUID, userID, workspace.ExternalID); !errors.Is(err, workspaceaccess.ErrDenied) {
		t.Fatalf("target workspace must be denied: %v", err)
	}
	base := "/api/organizations/" + refs.OrganizationUUID
	for _, path := range []string{base + "/workspaces/" + workspace.ExternalID + "/prompts", base + "/workspaces/" + workspace.UUID + "/prompts", base + "/workbench/prompts/" + promptID, base + "/workbench/prompts/" + promptID + "/revisions", base + "/workbench/prompts/" + promptID + "/kv_store/get/draft_revision"} {
		t.Run(path, func(t *testing.T) {
			response := app.platformRequestWithHeaders(t, http.MethodGet, path, nil, cookies, map[string]string{"X-Workspace-ID": "default"})
			defer response.Body.Close()
			body := readAll(t, response.Body)
			if response.StatusCode != http.StatusForbidden {
				t.Errorf("unauthorized target read = %d, leaked name = %t: %s", response.StatusCode, bytes.Contains(body, []byte("private-workspace-prompt")), body)
			}
		})
	}
	t.Run("unauthorized prompt update", func(t *testing.T) {
		response := app.platformRequestWithHeaders(t, http.MethodPut, base+"/workbench/prompts/"+promptID, strings.NewReader(`{"name":"outsider-change"}`), cookies, map[string]string{"X-Workspace-ID": "default"})
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("unauthorized target update = %d: %s", response.StatusCode, readAll(t, response.Body))
		}
	})
	t.Run("unauthorized prompt create and delete", func(t *testing.T) {
		for _, request := range []struct{ method, path string }{
			{http.MethodPost, base + "/workspaces/" + workspace.ExternalID + "/prompts"},
			{http.MethodDelete, base + "/workbench/prompts/" + promptID},
		} {
			response := app.platformRequestWithHeaders(t, request.method, request.path, strings.NewReader(`{}`), cookies, map[string]string{"X-Workspace-ID": "default"})
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s %s = %d", request.method, request.path, response.StatusCode)
			}
		}
		record, err := app.db.GetWorkbenchPrompt(t.Context(), refs.OrganizationUUID, promptID)
		if err != nil || record.Name != "private-workspace-prompt" || record.DeletedAt != nil {
			t.Fatalf("unauthorized request changed prompt: %+v, %v", record, err)
		}
	})
	t.Run("captured prompt cannot bypass workspace scope", func(t *testing.T) {
		capturedID := "52aa673d-8d88-408d-849d-e4c2a8e33144"
		_, err := app.db.UpsertWorkbenchPrompt(t.Context(), platform.WorkbenchPromptRecord{OrgUUID: refs.OrganizationUUID, PromptUUID: capturedID, WorkspaceUUID: workspace.UUID, WorkspaceDisplayID: workspace.ExternalID, Name: "private-captured-prompt"})
		if err != nil {
			t.Fatal(err)
		}
		list := app.platformRequestWithHeaders(t, http.MethodGet, base+"/workbench/prompts", nil, cookies, map[string]string{"X-Workspace-ID": "default"})
		defer list.Body.Close()
		if body := readAll(t, list.Body); list.StatusCode != http.StatusOK || bytes.Contains(body, []byte("private-captured-prompt")) {
			t.Fatalf("default list leaked captured prompt: %d %s", list.StatusCode, body)
		}
		create := app.platformRequestWithHeaders(t, http.MethodPost, base+"/workspaces/default/prompts", strings.NewReader(`{"name":"captured-overwrite"}`), cookies, map[string]string{"X-Workspace-ID": "default"})
		defer create.Body.Close()
		if create.StatusCode != http.StatusForbidden {
			t.Fatalf("create changed another workspace's captured prompt: %d", create.StatusCode)
		}
	})
	adminID := seedAdminUser(t, app.pool, "prompt-admin-"+uniqueAdminSuffix()+"@example.local", "admin")
	admin := auth.Principal{OrganizationUUID: refs.OrganizationUUID, UserExternalID: adminID}
	if _, err := workspaceaccess.ChangeMember(t.Context(), app.db, admin, workspace.ExternalID, userID, "workspace_user", "create"); err != nil {
		t.Fatal(err)
	}
	t.Run("authorized member can read and update", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			response := app.platformRequestWithHeaders(t, method, base+"/workbench/prompts/"+promptID, strings.NewReader(`{"name":"member-change"}`), cookies, map[string]string{"X-Workspace-ID": "default"})
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("authorized %s = %d", method, response.StatusCode)
			}
		}
		record, err := app.db.GetWorkbenchPrompt(t.Context(), refs.OrganizationUUID, promptID)
		if err != nil || record.Name != "member-change" {
			t.Fatalf("authorized update not persisted: %+v, %v", record, err)
		}
	})
	if _, err := workspaceaccess.ChangeMember(t.Context(), app.db, admin, workspace.ExternalID, userID, "", "delete"); err != nil {
		t.Fatal(err)
	}
	t.Run("revoked member cannot read", func(t *testing.T) {
		response := app.platformRequestWithHeaders(t, http.MethodGet, base+"/workbench/prompts/"+promptID, nil, cookies, map[string]string{"X-Workspace-ID": "default"})
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("revoked member read = %d", response.StatusCode)
		}
	})
	if _, err := workspaceaccess.ChangeMember(t.Context(), app.db, admin, workspace.ExternalID, userID, "workspace_user", "create"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.ArchiveAdminWorkspace(t.Context(), refs.OrganizationUUID, workspace.ExternalID); err != nil {
		t.Fatal(err)
	}
	t.Run("archived prompt detail", func(t *testing.T) {
		response := app.platformRequestWithHeaders(t, http.MethodGet, base+"/workbench/prompts/"+promptID, nil, cookies, map[string]string{"X-Workspace-ID": "default"})
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("archived target read = %d: %s", response.StatusCode, readAll(t, response.Body))
		}
	})
}
