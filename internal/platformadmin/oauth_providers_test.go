package platformadmin

import (
	"testing"
)

func TestValidateOAuthProviderFields(t *testing.T) {
	t.Parallel()
	if err := validateOAuthProviderFields("Example", "https://auth.example.test/authorize", "https://auth.example.test/token", "client"); err != nil {
		t.Fatalf("valid provider rejected: %v", err)
	}
	for _, test := range []struct {
		name, authorize, token, clientID string
	}{
		{"", "https://auth.example.test/authorize", "https://auth.example.test/token", "client"},
		{"Example", "not-a-url", "https://auth.example.test/token", "client"},
		{"Example", "https://auth.example.test/authorize", "ftp://auth.example.test/token", "client"},
		{"Example", "https://auth.example.test/authorize", "https://auth.example.test/token", ""},
	} {
		if err := validateOAuthProviderFields(test.name, test.authorize, test.token, test.clientID); err == nil {
			t.Fatalf("invalid provider accepted: %+v", test)
		}
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
