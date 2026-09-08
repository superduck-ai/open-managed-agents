package invitations

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type testStore struct {
	email  string
	id     string
	accept bool
	err    error
}

func (s *testStore) ListInvitations(_ context.Context, email string) ([]db.Invitation, error) {
	s.email = email
	return []db.Invitation{{ID: "invite_test", OrganizationUUID: "org_uuid", Email: email}}, s.err
}

func (s *testStore) RespondToInvitation(_ context.Context, id, email string, accept bool) (db.Invitation, error) {
	s.id, s.email, s.accept = id, email, accept
	status := "declined"
	if accept {
		status = "accepted"
	}
	return db.Invitation{ID: id, OrganizationUUID: "org_uuid", Status: status}, s.err
}

func TestInvitationHandlers(t *testing.T) {
	for _, test := range []struct {
		name, method, path string
		verified           bool
		err                error
		status             int
	}{
		{"无登录", "GET", "/api/invitations", false, nil, 401},
		{"无邀请", "POST", "/api/invitations/invite_test/accept", true, db.ErrNotFound, 404},
		{"已过期", "POST", "/api/invitations/invite_test/accept", true, db.ErrInvitationExpired, 409},
		{"已撤销", "POST", "/api/invitations/invite_test/accept", true, db.ErrInvitationRevoked, 409},
		{"终态冲突", "POST", "/api/invitations/invite_test/decline", true, db.ErrInvitationConflict, 409},
		{"列表", "GET", "/api/invitations", true, nil, 200},
		{"接受", "POST", "/api/invitations/invite_test/accept", true, nil, 200},
		{"拒绝", "POST", "/api/invitations/invite_test/decline", true, nil, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &testStore{err: test.err}
			handler := NewHandler(store, func(*http.Request) (string, bool) { return "verified@example.com", test.verified }, nil)
			router := chi.NewRouter()
			router.Route("/api/invitations", handler.RegisterRoutes)
			request := httptest.NewRequest(test.method, test.path+"?email=attacker@example.com", strings.NewReader(`{"email":"attacker@example.com"}`))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !test.verified && store.email != "" {
				t.Fatal("未登录调用了数据库")
			}
			if test.verified && store.email != "verified@example.com" {
				t.Fatalf("邮箱=%q", store.email)
			}
			if test.status != 200 {
				return
			}
			if strings.Contains(recorder.Body.String(), "email") || strings.Contains(recorder.Body.String(), "organization_id") {
				t.Fatalf("输出泄漏非合同字段: %s", recorder.Body.String())
			}
			if test.method == "POST" {
				var result responseResult
				if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.OrganizationUUID != "org_uuid" || store.id != "invite_test" || store.accept != strings.HasSuffix(test.path, "accept") {
					t.Fatalf("响应=%+v store=%+v", result, store)
				}
			}
		})
	}
}
