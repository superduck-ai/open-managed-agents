package vaults

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestValidationRedactsIssuedTokenDespiteInvalidSchema(t *testing.T) {
	for _, row := range []struct{ name, fields string }{
		{"overflow expiry", `,"expires_in":1125899906842624`},
		{"invalid token type", `,"token_type":17`},
		{"wrong access type", `,"access_token":17`},
		{"malformed tail", `,"expires_in":`},
	} {
		t.Run(row.name, func(t *testing.T) {
			fresh, refresh := "issued-access/+<private>", "issued-refresh/+<private>"
			accessJSON, _ := json.Marshal(fresh)
			refreshJSON, _ := json.Marshal(refresh)
			body := `{"access_token":` + string(accessJSON) + `,"refresh_token":` + string(refreshJSON) + row.fields + `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			env := &oauthRefreshEnv{t: t, now: oauthRefreshNow(), svc: newTestSecretsService(t), tokenURL: server.URL, client: server.Client()}
			cred := env.staleCred("synthetic-refresh")
			store := &fakeCredentialStore{get: cred}
			result, err := env.injector(store).validateMCPOAuth(t.Context(), cred)
			if err != nil || result.Status != "unknown" || result.Refresh.Status != "failed" || result.Refresh.HTTPResponse == nil || result.MCPProbe != nil || store.updateCalls != 0 {
				t.Fatal("invalid schema was accepted or diagnostic missing")
			}
			encoded, _ := json.Marshal(result)
			for _, value := range []string{fresh, refresh} {
				quoted, _ := json.Marshal(value)
				for _, form := range []string{value, string(quoted[1 : len(quoted)-1]), url.QueryEscape(value), base64.StdEncoding.EncodeToString([]byte(value))} {
					if strings.Contains(string(encoded), form) || strings.Contains(result.Refresh.HTTPResponse.Body, form) {
						t.Fatal("issued token escaped diagnostic redaction")
					}
				}
			}
		})
	}
}
