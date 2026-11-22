//go:build integration

package platformadmin

import (
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func TestWorkspaceScopeHTTPPostgres(t *testing.T) {
	database := administrationDatabase(t)
	service := New(database, nil)
	operator, secondAdmin, member := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	var foreignOrganization string
	err := database.WithPlatformAuthTx(t.Context(), func(tx db.PlatformAuthTxStore) error {
		for i, accounts := range [][]string{{operator, secondAdmin}, {member}} {
			org, err := tx.InsertOrganization(t.Context(), db.PlatformAuthOrganizationInput{Name: "Scope " + uuid.NewV4().String()})
			if err != nil {
				return err
			}
			if i == 1 {
				foreignOrganization = org.UUID
			}
			for _, account := range accounts {
				if _, err := tx.InsertUser(t.Context(), db.PlatformAuthUserInput{UUID: account, ExternalID: "user_" + account, OrganizationUUID: org.UUID, Email: account + "@example.com", Role: "admin"}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{operator, secondAdmin} {
		if err := service.SetAdministrator(t.Context(), account, "scope test", "", true); err != nil {
			t.Fatal(err)
		}
	}
	create := func(organization, externalID string, isDefault bool) db.AdminWorkspace {
		t.Helper()
		row, err := database.CreateAdminWorkspace(t.Context(), db.AdminWorkspace{
			UUID: uuid.NewV4().String(), ExternalID: externalID, OrganizationUUID: organization,
			Name: externalID, IsDefault: isDefault, CreatedAt: time.Now().UTC(), CompartmentID: uuid.NewV4().String(),
			DisplayColor: "#6C5BB9", DataResidency: []byte(`{}`), Tags: []byte(`{}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return row
	}
	active := create(foreignOrganization, "wrkspc_scope_active", false)
	defaultWorkspace := create(foreignOrganization, "wrkspc_scope_default", true)
	archived := create(foreignOrganization, "wrkspc_scope_archived", false)
	if _, err := database.ArchiveAdminWorkspace(t.Context(), foreignOrganization, archived.ExternalID); err != nil {
		t.Fatal(err)
	}
	orphan := create(uuid.NewV4().String(), "wrkspc_scope_orphan", false)
	create(foreignOrganization, "default", false)
	routes := NewHandler(service, func(r *http.Request) (platformsession.Session, error) {
		cookie, err := r.Cookie("sessionKey")
		if err != nil {
			return platformsession.Session{}, ErrUnauthenticated
		}
		return platformsession.Session{UserUUID: cookie.Value, VerifiedEmail: cookie.Value + "@example.com", AuthenticationMethod: "email_code_v1"}, nil
	}, true).Routes()
	request := func(account, workspaceID string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID+"/scope", nil)
		if account != "" {
			r.AddCookie(&http.Cookie{Name: "sessionKey", Value: account})
		}
		r.Header.Set("X-Organization-Uuid", uuid.NewV4().String())
		r.Header.Set("X-Workspace-Id", "wrkspc_other")
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("scope response must disable caching")
		}
		return w
	}
	t.Run("unauthorized requests are denied for every target", func(t *testing.T) {
		for _, account := range []string{"", member} {
			for _, id := range []string{active.ExternalID, "wrkspc_missing", "default"} {
				want := http.StatusForbidden
				if account == "" {
					want = http.StatusUnauthorized
				}
				if w := request(account, id); w.Code != want {
					t.Fatalf("status = %d, want = %d, body = %s", w.Code, want, w.Body.String())
				}
			}
		}
	})
	t.Run("inactive or nonexternal identifiers are not found", func(t *testing.T) {
		for _, id := range []string{"wrkspc_missing", "default", active.UUID, archived.ExternalID, orphan.ExternalID} {
			if _, err := database.GetPlatformAdminWorkspaceScope(t.Context(), id); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("identifier %q lookup error = %v", id, err)
			}
			if w := request(operator, id); w.Code != http.StatusNotFound {
				t.Fatalf("identifier %q status = %d, body = %s", id, w.Code, w.Body.String())
			}
		}
	})
	t.Run("nonmember administrator resolves active and real default workspaces", func(t *testing.T) {
		members, err := database.ListPlatformAdminMembers(t.Context(), db.PlatformAdminPageQuery{OrganizationUUID: foreignOrganization, Limit: 10})
		if err != nil || len(members) != 1 || members[0].UUID != member {
			t.Fatalf("foreign organization members = %+v, error = %v", members, err)
		}
		for _, workspace := range []db.AdminWorkspace{active, defaultWorkspace} {
			w := request(operator, workspace.ExternalID)
			var scope map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &scope); err != nil || w.Code != http.StatusOK || len(scope) != 2 || scope["workspace_id"] != workspace.ExternalID || scope["organization_uuid"] != foreignOrganization {
				t.Fatalf("scope status = %d, body = %s, error = %v", w.Code, w.Body.String(), err)
			}
		}
	})
	t.Run("revocation is checked again on the next request", func(t *testing.T) {
		if err := service.SetAdministrator(t.Context(), operator, "scope test", "", false); err != nil {
			t.Fatal(err)
		}
		if w := request(operator, active.ExternalID); w.Code != http.StatusForbidden {
			t.Fatalf("revoked administrator status = %d, body = %s", w.Code, w.Body.String())
		}
	})
}
