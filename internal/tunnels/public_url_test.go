package tunnels

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestCanonicalTunnelURLsUseConfiguredPublicOrigin(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "http://internal.example/request", nil)
	cfg := config.TunnelConfig{PublicBaseURL: "https://mcp.example.com"}
	if got := canonicalTunnelMCPURL(request, cfg, "tunnel_0123456789abcdef0123456789abcdef", "secondary"); got != "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/secondary" {
		t.Fatalf("canonical MCP URL = %q", got)
	}
	if got := canonicalTunnelOAuthMetadataURL(request, cfg, "tunnel_0123456789abcdef0123456789abcdef", "main"); got != "https://mcp.example.com/.well-known/oauth-protected-resource/v1/mcp/tunnel_0123456789abcdef0123456789abcdef" {
		t.Fatalf("canonical metadata URL = %q", got)
	}
}

func TestTunnelTargetRequiresExactConfiguredPublicOrigin(t *testing.T) {
	t.Parallel()
	cfg := config.TunnelConfig{PublicBaseURL: "https://mcp.example.com"}
	for rawURL, want := range map[string]bool{
		"https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef":      true,
		"https://MCP.EXAMPLE.COM/v1/mcp/tunnel_0123456789abcdef0123456789abcdef":      true,
		"http://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef":       false,
		"https://other.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef":    false,
		"https://mcp.example.com.evil/v1/mcp/tunnel_0123456789abcdef0123456789abcdef": false,
	} {
		target, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("parse %q: %v", rawURL, err)
		}
		if got := tunnelTargetUsesPublicOrigin(target, cfg); got != want {
			t.Fatalf("tunnelTargetUsesPublicOrigin(%q) = %v, want %v", rawURL, got, want)
		}
	}
}

func TestRecognizeTargetUsesCanonicalOriginAndHostnameAlias(t *testing.T) {
	t.Parallel()
	cfg := config.TunnelConfig{PublicBaseURL: "https://mcp.example.com", DomainSuffix: "tunnel.example"}
	for _, test := range []struct {
		name       string
		rawURL     string
		recognized bool
		wantError  bool
		want       TargetReference
	}{
		{
			name:       "failure malformed canonical target",
			rawURL:     "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef?query=1",
			recognized: true,
			wantError:  true,
		},
		{
			name:       "failure invalid canonical tunnel ID",
			rawURL:     "https://mcp.example.com/v1/mcp/tunnel_not-an-id",
			recognized: true,
			wantError:  true,
		},
		{
			name:       "failure invalid canonical channel",
			rawURL:     "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/UPPER",
			recognized: true,
			wantError:  true,
		},
		{
			name:       "failure invalid hostname channel",
			rawURL:     "https://private.tunnel.example/UPPER",
			recognized: true,
			wantError:  true,
		},
		{
			name:       "success canonical channel",
			rawURL:     "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/secondary",
			recognized: true,
			want: TargetReference{
				TunnelID: "tunnel_0123456789abcdef0123456789abcdef",
				Channel:  "secondary",
			},
		},
		{
			name:       "success hostname alias",
			rawURL:     "https://private.tunnel.example/main",
			recognized: true,
			want:       TargetReference{Domain: "private.tunnel.example", Channel: "main"},
		},
		{
			name:   "success ordinary third party path remains unrecognized",
			rawURL: "https://third-party.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, err := url.Parse(test.rawURL)
			if err != nil {
				t.Fatalf("parse target: %v", err)
			}
			got, recognized, err := RecognizeTarget(target, cfg)
			if (err != nil) != test.wantError {
				t.Fatalf("RecognizeTarget() error = %v, wantError %v", err, test.wantError)
			}
			if recognized != test.recognized || got != test.want {
				t.Fatalf("RecognizeTarget() = (%+v, %v), want (%+v, %v)", got, recognized, test.want, test.recognized)
			}
		})
	}
}

func TestOAuthResponseAndChallengeRewritePublicURLs(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	response := TunnelResponse{
		ResponseCode: 200,
		JSONResponse: []byte(`{"resource":"http://127.0.0.1/private","authorization_servers":["https://auth.example.com"]}`),
	}
	if err := writeOAuthDiscoveryResponse(recorder, response, "https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("write OAuth response: %v", err)
	}
	if body := recorder.Body.String(); body != "{\"authorization_servers\":[\"https://auth.example.com\"],\"resource\":\"https://mcp.example.com/v1/mcp/tunnel_0123456789abcdef0123456789abcdef\"}\n" {
		t.Fatalf("OAuth response = %q", body)
	}
	challenge := `Bearer realm="mcp", resource_metadata="http://127.0.0.1/private"`
	if got := rewriteResourceMetadata(challenge, "https://mcp.example.com/metadata"); got != `Bearer realm="mcp", resource_metadata="https://mcp.example.com/metadata"` {
		t.Fatalf("rewritten challenge = %q", got)
	}
}
