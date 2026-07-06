package tests

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestConsoleMembersAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("console-members-bucket"))
	defer app.close()
	cookies := app.platformLoginCookies(t, "console-members@example.com")

	var orgUUID string
	var orgID int64
	var userUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select o.uuid::text, o.id
		from organizations o
		where o.external_id = 'org_default'
	`).Scan(&orgUUID, &orgID); err != nil {
		t.Fatalf("load default organization ids: %v", err)
	}
	suffix := uniqueAdminSuffix()
	email := "console-member-" + suffix + "@example.com"
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into users (external_id, organization_id, email, name, role)
		values ($1, $2, $3, $4, 'admin')
		returning uuid::text
	`, "user_console_"+suffix, orgID, email, "Console Member").Scan(&userUUID); err != nil {
		t.Fatalf("seed console member: %v", err)
	}
	memberID := consoleTaggedUserID(userUUID)
	path := "/api/console/organizations/" + orgUUID + "/members"

	t.Run("success list members by organization uuid", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var members []map[string]any
		decodeJSON(t, resp.Body, &members)
		member := findConsoleMember(members, memberID)
		if member == nil {
			t.Fatalf("member %q not found in %#v", memberID, members)
		}
		if member["id"] != memberID ||
			member["type"] != "user" ||
			member["email"] != email ||
			member["name"] != "Console Member" ||
			member["role"] != "admin" ||
			member["added_at"] == "" {
			t.Fatalf("member response mismatch: %#v", member)
		}
	})

	t.Run("success update and delete member", func(t *testing.T) {
		updateResp := app.doPlatformConsole(t, http.MethodPost, path+"/"+memberID, []byte(`{"role":"developer"}`), cookies)
		defer updateResp.Body.Close()
		if updateResp.StatusCode != http.StatusOK {
			t.Fatalf("update status = %d, want 200: %s", updateResp.StatusCode, readAll(t, updateResp.Body))
		}
		var updated map[string]any
		decodeJSON(t, updateResp.Body, &updated)
		if updated["id"] != memberID || updated["role"] != "developer" {
			t.Fatalf("updated member mismatch: %#v", updated)
		}

		deleteResp := app.doPlatformConsole(t, http.MethodDelete, path+"/"+memberID, nil, cookies)
		defer deleteResp.Body.Close()
		if deleteResp.StatusCode != http.StatusOK {
			t.Fatalf("delete status = %d, want 200: %s", deleteResp.StatusCode, readAll(t, deleteResp.Body))
		}
		var deleted map[string]any
		decodeJSON(t, deleteResp.Body, &deleted)
		if deleted["id"] != memberID || deleted["type"] != "user_deleted" {
			t.Fatalf("deleted member mismatch: %#v", deleted)
		}

		listResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer listResp.Body.Close()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("list after delete status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
		}
		var members []map[string]any
		decodeJSON(t, listResp.Body, &members)
		if member := findConsoleMember(members, memberID); member != nil {
			t.Fatalf("member after delete = %#v, want absent", member)
		}
	})
}

func (a *testApp) doPlatformConsole(t *testing.T, method string, path string, body []byte, cookies []*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, a.baseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = "platform.claude.com"
	req.Header.Set("anthropic-version", "2023-06-01")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func consoleTaggedUserID(userUUID string) string {
	compact := strings.ReplaceAll(userUUID, "-", "")
	if len(compact) > 24 {
		compact = compact[:24]
	}
	return "user_" + compact
}

func findConsoleMember(members []map[string]any, id string) map[string]any {
	for _, member := range members {
		if member["id"] == id {
			return member
		}
	}
	return nil
}
