package platformadmin

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestAdministrationHTTPPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := administrationService(t, database)
	operator, ordinary := uuid.NewV4().String(), uuid.NewV4().String()
	if err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "注册组织"})
		if err != nil {
			return err
		}
		for _, account := range []string{operator, ordinary} {
			if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: account, ExternalID: "user_" + account, OrganizationUUID: org.UUID, Email: account + "@example.com", Role: "admin"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetAdministrator(t.Context(), operator, "测试运维", "", true); err != nil {
		t.Fatal(err)
	}
	routes := NewHandler(service, func(r *http.Request) (platformsession.Session, error) {
		cookie, err := r.Cookie("sessionKey")
		if err != nil || (cookie.Value != operator && cookie.Value != ordinary) {
			return platformsession.Session{}, ErrUnauthenticated
		}
		return platformsession.Session{UserUUID: cookie.Value, VerifiedEmail: cookie.Value + "@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true).Routes()
	request := func(method, path, account string, body any, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		var payload []byte
		if body != nil {
			var err error
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(payload))
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: account})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "http-"+path)
		r.Header.Set("X-Organization-Uuid", uuid.NewV4().String())
		r.Header.Set("X-Workspace-Id", "forged-workspace")
		if csrf {
			r.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(account))
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("后台响应允许缓存")
		}
		return w
	}
	wantStatus := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("HTTP 状态 = %d，预期 %d：%s", w.Code, status, w.Body.String())
		}
	}
	input := CreateOrganization{Name: "HTTP 受管组织", AdminAccountUUIDs: []string{ordinary}}
	t.Run("先鉴权再检查目标且写入要求CSRF", func(t *testing.T) {
		wantStatus(request("GET", "/organizations/not-a-uuid", "", nil, false), 401)
		wantStatus(request("GET", "/organizations/not-a-uuid", ordinary, nil, false), 403)
		wantStatus(request("GET", "/organizations/"+uuid.NewV4().String(), ordinary, nil, false), 403)
		wantStatus(request("POST", "/organizations", operator, input, false), 403)
	})
	t.Run("中文账号搜索按字符校验", func(t *testing.T) {
		wantStatus(request("GET", "/accounts?query="+url.QueryEscape("张"), operator, nil, false), 400)
		wantStatus(request("GET", "/accounts?query="+url.QueryEscape(strings.Repeat("张", 201)), operator, nil, false), 400)
		wantStatus(request("GET", "/accounts?query="+url.QueryEscape(strings.Repeat("张", 200)), operator, nil, false), 200)
	})
	w := request("POST", "/organizations", operator, input, true)
	wantStatus(w, 201)
	var org Organization
	if err := json.Unmarshal(w.Body.Bytes(), &org); err != nil {
		t.Fatal(err)
	}
	path := "/organizations/" + org.UUID
	t.Run("创建与幂等重放及真实Default", func(t *testing.T) {
		if org.AdminCount != 1 || org.MemberCount != 1 || org.WorkspaceCount != 1 {
			t.Fatalf("创建不完整：%+v", org)
		}
		replay := request("POST", "/organizations", operator, input, true)
		wantStatus(replay, 200)
		var repeated Organization
		if err := json.Unmarshal(replay.Body.Bytes(), &repeated); err != nil || repeated.UUID != org.UUID {
			t.Fatalf("重复创建：%+v，%v", repeated, err)
		}
		wantStatus(request("GET", path, operator, nil, false), 200)
		workspaces := request("GET", path+"/workspaces", operator, nil, false)
		wantStatus(workspaces, 200)
		var listed page[Workspace]
		if err := json.Unmarshal(workspaces.Body.Bytes(), &listed); err != nil || len(listed.Data) != 1 || !listed.Data[0].IsDefault {
			t.Fatalf("Default 工作区错误：%s，%v", workspaces.Body.String(), err)
		}
	})
	t.Run("其他组织成员UUID及未知字段不得写入", func(t *testing.T) {
		change := AdministratorChange{Change: Change{ExpectedVersion: org.Version, Reason: "跨组织请求"}, Role: "user"}
		wantStatus(request("PATCH", path+"/administrators/"+ordinary, operator, change, true), 404)
		wantStatus(request("PATCH", path, operator, map[string]any{"expected_version": org.Version, "reason": "伪造字段", "platform_role": "super_admin"}, true), 400)
		after, err := database.GetPlatformAdminOrganization(t.Context(), org.UUID)
		if err != nil || after.Version != org.Version || after.AdminCount != 1 {
			t.Fatalf("拒绝请求修改了组织：%+v，%v", after, err)
		}
	})
	t.Run("待接受邀请不增加有效管理员且邀请不能跨组织撤销", func(t *testing.T) {
		invite := InviteChange{Change: Change{ExpectedVersion: org.Version, Reason: "邀请验收"}, Email: "invitee@example.com", Role: "admin"}
		created := request("POST", path+"/invitations", operator, invite, true)
		wantStatus(created, 200)
		if err := json.Unmarshal(created.Body.Bytes(), &org); err != nil || org.AdminCount != 1 {
			t.Fatalf("邀请错误计为管理员：%s，%v", created.Body.String(), err)
		}
		invites := request("GET", path+"/invitations", operator, nil, false)
		wantStatus(invites, 200)
		var listed page[Invite]
		if err := json.Unmarshal(invites.Body.Bytes(), &listed); err != nil || len(listed.Data) != 1 || listed.Data[0].DeliveryStatus != "pending" {
			t.Fatalf("邀请或待发送记录缺失：%s，%v", invites.Body.String(), err)
		}
		other, _, err := service.CreateOrganization(t.Context(), Actor{AccountUUID: operator}, CreateOrganization{Name: "其他组织", Reason: "范围验收", AdminAccountUUIDs: []string{ordinary}}, "other-org")
		if err != nil {
			t.Fatal(err)
		}
		wantStatus(request("POST", "/organizations/"+other.UUID+"/invitations/"+listed.Data[0].UUID+"/revoke", operator, Change{ExpectedVersion: other.Version, Reason: "跨组织邀请"}, true), 404)
		wantStatus(request("POST", path+"/invitations/"+listed.Data[0].UUID+"/revoke", operator, Change{ExpectedVersion: org.Version, Reason: "撤销验收"}, true), 200)
	})
	t.Run("邀请拒绝旧角色且接受后保留User或Admin", func(t *testing.T) {
		for _, role := range []string{"", "developer", "billing", "claude_code_user", "super_admin"} {
			wantStatus(request("POST", path+"/invitations", operator, InviteChange{
				Change: Change{ExpectedVersion: org.Version, Reason: "非法角色"}, Email: "invalid@example.com", Role: role,
			}, true), 400)
		}
		for _, role := range []string{"user", "admin"} {
			before, err := database.GetPlatformAdminOrganization(t.Context(), org.UUID)
			if err != nil {
				t.Fatal(err)
			}
			email := role + "-role@example.com"
			wantStatus(request("POST", path+"/invitations", operator, InviteChange{
				Change: Change{ExpectedVersion: before.Version}, Email: email, Role: role,
			}, true), 200)
			response := request("GET", path+"/invitations", operator, nil, false)
			wantStatus(response, 200)
			var listed page[Invite]
			if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			var invite Invite
			for _, row := range listed.Data {
				if row.Email == email {
					invite = row
				}
			}
			if invite.Role != role {
				t.Fatalf("邀请角色 = %q，预期 %q", invite.Role, role)
			}
			if _, err := database.RespondToInvitation(t.Context(), invite.ID, "wrong@example.com", true); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("其他邮箱接受邀请错误 = %v", err)
			}
			pending, err := database.GetPlatformAdminOrganization(t.Context(), org.UUID)
			if err != nil || pending.MemberCount != before.MemberCount || pending.AdminCount != before.AdminCount {
				t.Fatalf("未接受邀请改变成员权限：%+v，%v", pending, err)
			}
			if _, err := database.RespondToInvitation(t.Context(), invite.ID, email, true); err != nil {
				t.Fatal(err)
			}
			members, err := database.ListPlatformAdminMembers(t.Context(), db.PlatformAdminPageQuery{OrganizationUUID: org.UUID, Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, member := range members {
				if member.Email == email {
					found = true
					if member.Role != role {
						t.Fatalf("接受后角色 = %q，预期 %q", member.Role, role)
					}
				}
			}
			if !found {
				t.Fatal("接受后未创建组织成员")
			}
			after, err := database.GetPlatformAdminOrganization(t.Context(), org.UUID)
			admins := before.AdminCount
			if role == "admin" {
				admins++
			}
			if err != nil || after.MemberCount != before.MemberCount+1 || after.AdminCount != admins {
				t.Fatalf("接受后成员统计错误：%+v，%v", after, err)
			}
		}
	})

}
