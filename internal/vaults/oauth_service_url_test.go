package vaults

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestOAuthServiceURLSurvivesRepeatedRefresh(t *testing.T) {
	const target = "https://open.feishu.cn/open-apis"
	const service = "https://bridge.example"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if r.Form.Get("client_id") != "bridge-client" || r.Form.Get("client_secret") != "bridge-secret" || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != fmt.Sprintf("refresh-%d", calls) {
			t.Error("refresh used incorrect bridge identity or rotated token")
			w.WriteHeader(400)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}`, calls, calls)
	}))
	defer server.Close()
	raw, secretRaw, err := BuildMCPOAuthStoredCredentialJSON(MCPOAuthStoredCredentialInput{
		MCPServerURL: target, OAuthServiceURL: service, AccessToken: "access-0", RefreshToken: "refresh-0", TokenEndpoint: server.URL,
		ClientID: "bridge-client", ClientSecret: "bridge-secret", ClientCredentialSource: MCPOAuthClientCredentialPlatform, TokenEndpointAuthMethod: "client_secret_post",
	})
	if err != nil {
		t.Fatal(err)
	}
	clients := []config.PlatformOAuthClientConfig{
		{MCPServerURL: target, ClientID: "wrong-client", ClientSecret: "wrong-secret"},
		{MCPServerURL: service, ClientID: "bridge-client", ClientSecret: "bridge-secret"},
	}
	for range 2 {
		auth, err := decodeMCPOAuthCredentialAuth(raw)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := decodeMCPOAuthCredentialSecret(secretRaw)
		if err != nil {
			t.Fatal(err)
		}
		_, raw, secretRaw, err = exchangeMCPOAuthRefresh(t.Context(), server.Client(), auth, secret, time.Now(), clients)
		if err != nil {
			t.Fatal(err)
		}
		auth, err = decodeMCPOAuthCredentialAuth(raw)
		if err != nil {
			t.Fatal(err)
		}
		if auth.OAuthServiceURL != service || auth.MCPServerURL != target || auth.Refresh.TokenEndpoint != server.URL {
			t.Fatal("refresh lost URL separation")
		}
		if strings.Contains(string(raw), "bridge-secret") || strings.Contains(string(secretRaw), "bridge-secret") {
			t.Fatal("platform secret persisted")
		}
	}
	if calls != 2 {
		t.Fatalf("refresh calls = %d", calls)
	}
}

func TestOAuthServiceURLOnlyInjectsAtCredentialTarget(t *testing.T) {
	raw, _, err := BuildMCPOAuthStoredCredentialJSON(MCPOAuthStoredCredentialInput{MCPServerURL: "https://open.feishu.cn/open-apis", OAuthServiceURL: "https://bridge.example", AccessToken: "access"})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://bridge.example/token", "https://open.feishu.cn/other", "https://open.feishu.cn/open-apis/authen/v1/user_info"} {
		requestURL, err := url.Parse(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		matches, _, err := listInjectableMatches(requestURL, []db.VaultCredential{{Auth: raw}})
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if endpoint == "https://open.feishu.cn/open-apis/authen/v1/user_info" {
			want = 1
		}
		if len(matches) != want {
			t.Fatalf("%s matched %d credentials, want %d", endpoint, len(matches), want)
		}
	}
}
