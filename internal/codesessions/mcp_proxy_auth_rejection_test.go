package codesessions

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestMCPProxyPreservesExhaustedUpstreamAuthenticationCategory(t *testing.T) {
	for _, row := range []struct {
		name   string
		cause  error
		status int
	}{
		{"upstream401", vaults.ErrUpstreamAuthenticationRejected, http.StatusUnauthorized},
		{"oauth reauthorization", vaults.ErrMCPOAuthReauthorizationRequired, http.StatusUnauthorized},
		{"generic injection", errors.New("generic injection failure"), http.StatusBadGateway},
	} {
		t.Run(row.name, func(t *testing.T) {
			const targetURL = "https://mcp.example.com/mcp"
			handler, token := newMCPProxyTestHandler(t, targetURL, slog.Default())
			handler.wrapMCPVaultTransport = func(context.Context, SessionCredentialClaims, *url.URL, http.RoundTripper) http.RoundTripper {
				return roundTripFunc(func(*http.Request) (*http.Response, error) {
					return nil, errors.Join(vaults.ErrInjectionRejected, row.cause, errors.New("private credential diagnostic"))
				})
			}
			router := chi.NewRouter()
			router.Route("/v2", handler.RegisterV2Routes)
			srv := httptest.NewServer(router)
			defer srv.Close()
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/v2/ccr-sessions/cse_test/mcp?"+url.Values{"mcp_url": {targetURL}}.Encode(), bytes.NewBufferString(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != row.status {
				t.Fatalf("status = %d, want%d", resp.StatusCode, row.status)
			}
			if (row.status == 401 && !strings.Contains(string(b), `"authentication_error"`)) || strings.Contains(string(b), "private credential diagnostic") {
				t.Fatalf("unsafe or incorrect public error: %s", b)
			}
		})
	}
}
