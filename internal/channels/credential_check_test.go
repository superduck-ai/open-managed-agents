package channels

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestCheckAppCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "bad-secret") {
			_, _ = w.Write([]byte(`{"code":10014,"msg":"app secret invalid"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"t-secret","expire":7200}`))
	}))
	defer upstream.Close()
	previous := feishuTokenURL
	feishuTokenURL = upstream.URL
	t.Cleanup(func() { feishuTokenURL = previous })

	router := chi.NewRouter()
	router.Mount("/channels", NewWorkspaceHandler(config.ChannelBridgeConfig{}, scopedStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))

	ok := credentialCheckRequest(t, router, `{"app_id":"cli_aa6c7433e1b8dbf1","app_secret":"good-secret"}`, true)
	if ok.Code != http.StatusOK || strings.Contains(ok.Body.String(), "tenant_access_token") || strings.Contains(ok.Body.String(), "good-secret") {
		t.Fatalf("body = %s", ok.Body.String())
	}
	rejected := credentialCheckRequest(t, router, `{"app_id":"cli_aa6c7433e1b8dbf1","app_secret":"bad-secret"}`, true)
	if rejected.Code != http.StatusBadRequest || strings.Contains(rejected.Body.String(), "bad-secret") {
		t.Fatalf("rejected = %d %s", rejected.Code, rejected.Body.String())
	}
	invalid := credentialCheckRequest(t, router, `{"app_id":"not-an-app","app_secret":"good-secret"}`, true)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid = %d", invalid.Code)
	}
	anonymous := credentialCheckRequest(t, router, `{"app_id":"cli_aa6c7433e1b8dbf1","app_secret":"good-secret"}`, false)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", anonymous.Code)
	}
}

func credentialCheckRequest(t *testing.T, router http.Handler, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/channels/credentials/check", strings.NewReader(body))
	if authenticated {
		request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
