package platformapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type memberActorFixture struct{ role string }

func (f memberActorFixture) GetAdminUser(context.Context, string, string) (db.AdminUser, error) {
	return db.AdminUser{Role: f.role}, nil
}

func TestConsoleOrganizationAdminAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, role, organization string
		status                   int
	}{
		{"拒绝普通成员", "user", "org", 403},
		{"拒绝工作区管理员", "developer", "org", 403},
		{"拒绝计费成员", "billing", "org", 403},
		{"拒绝其他组织", "admin", "other", 404},
		{"允许当前组织管理员", "admin", "org", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := chi.NewRouter()
			router.Route("/organizations/{orgUuid}", func(r chi.Router) {
				r.Use(requireConsoleOrganizationAdmin(memberActorFixture{role: tc.role}))
				r.Delete("/members/user", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			})
			req := httptest.NewRequest(http.MethodDelete, "/organizations/"+tc.organization+"/members/user", nil)
			req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{OrganizationUUID: "org", UserExternalID: "actor"}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
		})
	}
}

func TestConsoleMemberErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{db.ErrLastOrganizationAdmin, 409},
		{db.ErrNotFound, 404},
		{errConsoleOrganizationAdminRequired, 403},
		{errors.New("private cause"), 500},
	} {
		response := httptest.NewRecorder()
		writeConsoleMemberError(response, tc.err)
		if response.Code != tc.status {
			t.Fatalf("status = %d, want %d", response.Code, tc.status)
		}
	}
}
