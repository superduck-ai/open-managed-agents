package platformadmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestWorkspaceScopeAuthorizationBeforeLookup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		loadErr    error
		verifyErr  error
		unverified bool
		disabled   bool
		credential string
		want       int
	}{
		{name: "missing session", loadErr: ErrUnauthenticated, want: 401},
		{name: "unverified login", unverified: true, want: 403},
		{name: "disabled administration", disabled: true, want: 403},
		{name: "ordinary account", verifyErr: ErrDenied, want: 403},
		{name: "revoked administrator", verifyErr: ErrDenied, want: 403},
		{name: "API key", credential: "X-Api-Key", want: 403},
		{name: "Bearer token", credential: "Authorization", want: 403},
		{name: "authorization unavailable", verifyErr: ErrUnavailable, want: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := platformsession.Session{UserUUID: "account", VerifiedEmail: "admin@example.com", AuthenticationMethod: "email_code_v1"}
			if tc.unverified {
				session.AuthenticationMethod = ""
			}
			h := NewHandler(New(nil, nil), func(*http.Request) (platformsession.Session, error) { return session, tc.loadErr }, !tc.disabled)
			h.verify = func(context.Context, string) (db.PlatformAdminAccount, error) {
				return db.PlatformAdminAccount{UUID: session.UserUUID}, tc.verifyErr
			}
			for _, id := range []string{"wrkspc_target", "wrkspc_missing", "default"} {
				r := httptest.NewRequest(http.MethodGet, "/workspaces/"+id+"/scope", nil)
				if tc.credential != "" {
					r.Header.Set(tc.credential, "rejected-credential")
				}
				w := httptest.NewRecorder()
				h.Routes().ServeHTTP(w, r)
				if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("workspace %q status = %d, want = %d, body = %s", id, w.Code, tc.want, w.Body.String())
				}
			}
		})
	}
}
