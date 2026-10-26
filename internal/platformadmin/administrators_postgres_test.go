//go:build integration

package platformadmin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestSuperAdministratorManagementPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := New(database, nil)
	ctx := t.Context()
	operator, target, legacy := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	if err := database.WithPlatformAuthTx(ctx, func(tx db.PlatformAuthTxStore) error {
		for _, id := range []string{operator, target, legacy} {
			org, err := tx.InsertOrganization(ctx, db.PlatformAuthOrganizationInput{Name: "账号来源"})
			if err != nil {
				return err
			}
			if _, err := tx.InsertUser(ctx, db.PlatformAuthUserInput{UUID: id, ExternalID: "user_" + id, OrganizationUUID: org.UUID, Email: id + "@example.com", Role: "admin"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{operator, legacy} {
		if err := service.SetAdministrator(ctx, id, "初始化运维", "", true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.SQLDB().ExecContext(ctx, "UPDATE users SET email = $1, registration_identity = false WHERE uuid = $2", operator+"@example.com", legacy); err != nil {
		t.Fatal(err)
	}
	routes := NewHandler(service, func(r *http.Request) (platformsession.Session, error) {
		cookie, err := r.Cookie("sessionKey")
		if err != nil {
			return platformsession.Session{}, ErrUnauthenticated
		}
		return platformsession.Session{UserUUID: cookie.Value, VerifiedEmail: cookie.Value + "@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true).Routes()
	request := func(method, path, id, body string, csrf bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "sessionKey", Value: id})
		if csrf {
			r.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken(id))
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s：HTTP %d，预期 %d：%s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	body := `{"account_uuid":"` + target + `"}`
	t.Run("普通组织管理员不能访问或自提权且写入要求CSRF", func(t *testing.T) {
		request("GET", "/administrators", target, "", false, 403)
		request("POST", "/administrators", target, body, true, 403)
		request("DELETE", "/administrators/"+operator, target, "", true, 403)
		request("POST", "/administrators", operator, body, false, 403)
		request("DELETE", "/administrators/"+operator, operator, "", false, 403)
		if err := service.ChangeAdministrator(ctx, Actor{AccountUUID: target}, target, true); !errors.Is(err, ErrDenied) {
			t.Fatalf("事务内未拒绝无权操作者：%v", err)
		}
	})
	t.Run("旧身份授权不计入最后有效管理员保护", func(t *testing.T) {
		listed := request("GET", "/administrators", operator, "", false, 200)
		if strings.Contains(listed.Body.String(), legacy) || !strings.Contains(listed.Body.String(), operator) {
			t.Fatalf("管理员目录包含旧身份：%s", listed.Body.String())
		}
		request("DELETE", "/administrators/"+operator, operator, "", true, 409)
		if err := service.SetAdministrator(ctx, operator, "运维", "", false); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("CLI 最后管理员保护失效：%v", err)
		}
	})
	t.Run("新增授权即时生效且重复授权幂等", func(t *testing.T) {
		request("POST", "/administrators", operator, `{"account_uuid":"invalid"}`, true, 400)
		request("POST", "/administrators", operator, `{"account_uuid":"`+uuid.NewV4().String()+`"}`, true, 404)
		request("POST", "/administrators", operator, `{"account_uuid":"`+legacy+`"}`, true, 404)
		request("POST", "/administrators", operator, `{"account_uuid":"`+target+`","operator":"forged"}`, true, 400)
		request("POST", "/administrators", operator, body, true, 200)
		request("POST", "/administrators", operator, body, true, 200)
		request("GET", "/administrators", target, "", false, 200)
		var grantedBy, role string
		if err := database.SQLDB().QueryRowContext(ctx, "SELECT a.granted_by, u.role FROM platform_admins a JOIN users u ON u.uuid=a.account_uuid WHERE u.uuid=$1", target).Scan(&grantedBy, &role); err != nil || grantedBy != operator || role != "admin" {
			t.Fatalf("操作者身份或组织角色错误：%q %q %v", grantedBy, role, err)
		}
	})
	t.Run("撤权即时生效且旧请求在事务内被拒绝", func(t *testing.T) {
		request("DELETE", "/administrators/"+target, operator, "", true, 200)
		request("DELETE", "/administrators/"+target, operator, "", true, 200)
		request("GET", "/administrators", target, "", false, 403)
		if err := service.ChangeAdministrator(ctx, Actor{AccountUUID: target}, target, true); !errors.Is(err, ErrDenied) {
			t.Fatalf("已撤权操作者仍能写入：%v", err)
		}
		request("POST", "/administrators", operator, body, true, 200)
	})
	t.Run("有其他有效管理员时允许撤销自身且立即失去后台访问", func(t *testing.T) {
		request("DELETE", "/administrators/"+target, target, "", true, 200)
		request("GET", "/administrators", target, "", false, 403)
		request("POST", "/administrators", operator, body, true, 200)
	})
	t.Run("CLI与后台并发撤权仍保留一位有效管理员", func(t *testing.T) {
		start, results := make(chan struct{}), make(chan error, 2)
		var workers sync.WaitGroup
		workers.Go(func() { <-start; results <- service.SetAdministrator(ctx, operator, "并发运维", "", false) })
		workers.Go(func() {
			<-start
			results <- service.ChangeAdministrator(ctx, Actor{AccountUUID: target}, target, false)
		})
		close(start)
		workers.Wait()
		close(results)
		success, protected := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, ErrLastAdmin) {
				protected++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || protected != 1 {
			t.Fatalf("并发撤权：成功 %d，保护 %d", success, protected)
		}
		admins, err := service.Administrators(ctx)
		if err != nil || len(admins) != 1 {
			t.Fatalf("有效管理员列表：%+v %v", admins, err)
		}
	})
}
