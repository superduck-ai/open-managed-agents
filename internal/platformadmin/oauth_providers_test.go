package platformadmin

import (
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestValidateOAuthProviderFields(t *testing.T) {
	t.Parallel()
	if err := validateOAuthProviderFields("Example", "https://auth.example.test/authorize", "https://auth.example.test/token", config.EnvironmentDev); err != nil {
		t.Fatalf("valid provider rejected: %v", err)
	}
	for _, test := range []struct {
		name, authorize, token string
	}{
		{"", "https://auth.example.test/authorize", "https://auth.example.test/token"},
		{"Example", "not-a-url", "https://auth.example.test/token"},
		{"Example", "https://auth.example.test/authorize", "ftp://auth.example.test/token"},
	} {
		if err := validateOAuthProviderFields(test.name, test.authorize, test.token, config.EnvironmentDev); err == nil {
			t.Fatalf("invalid provider accepted: %+v", test)
		}
	}
}

func TestValidateOAuthProviderFieldsCountsRunes(t *testing.T) {
	t.Parallel()
	if err := validateOAuthProviderFields(strings.Repeat("客", 200), "https://auth.example.test/authorize", "https://auth.example.test/token", config.EnvironmentDev); err != nil {
		t.Fatalf("200-character name rejected: %v", err)
	}
	if err := validateOAuthProviderFields(strings.Repeat("客", 201), "https://auth.example.test/authorize", "https://auth.example.test/token", config.EnvironmentDev); err == nil {
		t.Fatal("over-long name accepted")
	}
}

func TestValidateOAuthProviderFieldsRejectsHTTPInProduction(t *testing.T) {
	t.Parallel()
	if err := validateOAuthProviderFields("Example", "https://auth.example.test/authorize", "http://auth.example.test/token", config.EnvironmentDev); err != nil {
		t.Fatalf("dev provider rejected: %v", err)
	}
	if err := validateOAuthProviderFields("Example", "https://auth.example.test/authorize", "http://auth.example.test/token", config.EnvironmentProd); err == nil {
		t.Fatal("production provider accepted http token endpoint")
	}
}

func TestOAuthProviderAuthMethodAndEncoding(t *testing.T) {
	t.Parallel()
	if method, err := oauthProviderAuthMethod(nil, "client_secret_post"); err != nil || method != "client_secret_post" {
		t.Fatalf("default method = %q: %v", method, err)
	}
	for _, method := range []string{"client_secret_post", "client_secret_basic", "none"} {
		value := method
		if got, err := oauthProviderAuthMethod(&value, ""); err != nil || got != method {
			t.Fatalf("method %q = %q: %v", method, got, err)
		}
	}
	invalidMethod := "unknown"
	if _, err := oauthProviderAuthMethod(&invalidMethod, ""); err == nil {
		t.Fatal("invalid method accepted")
	}
	if encoding, err := oauthProviderEncoding(nil, "form"); err != nil || encoding != "form" {
		t.Fatalf("default encoding = %q: %v", encoding, err)
	}
	for _, encoding := range []string{"form", "json"} {
		value := encoding
		if got, err := oauthProviderEncoding(&value, ""); err != nil || got != encoding {
			t.Fatalf("encoding %q = %q: %v", encoding, got, err)
		}
	}
	invalidEncoding := "msgpack"
	if _, err := oauthProviderEncoding(&invalidEncoding, ""); err == nil {
		t.Fatal("invalid encoding accepted")
	}
}

func TestNormalizeOAuthProviderLogoURL(t *testing.T) {
	for _, value := range []string{"/logo.png", "javascript:alert(1)", "data:image/png;base64,AAA", "https://user:password@example.test/logo.png"} {
		if _, err := normalizeOAuthProviderLogoURL(value); err == nil {
			t.Fatalf("非法 Logo URL 被接受：%q", value)
		}
	}
	for _, value := range []string{"", "https://example.test/logo.svg", "http://localhost/logo.png"} {
		if normalized, err := normalizeOAuthProviderLogoURL(" " + value + " "); err != nil || normalized != value {
			t.Fatalf("可选 Logo URL 被拒绝：%q，%v", value, err)
		}
	}
}
