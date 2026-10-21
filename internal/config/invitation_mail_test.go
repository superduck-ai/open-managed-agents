package config

import "testing"

func TestInvitationConsoleURL(t *testing.T) {
	for _, input := range []string{"http://oma.example", "https://user:password@oma.example", "https://oma.example/path", "https://oma.example?redirect=evil", "https://oma.example#hash", "//oma.example"} {
		if err := validateAuthConfig(EnvironmentProd, AuthConfig{ConsoleURL: input}); err == nil {
			t.Errorf("接受了不安全的公开地址 %s", input)
		}
	}
	for _, input := range []string{"", "https://oma.example", "https://oma.example/"} {
		if err := validateAuthConfig(EnvironmentProd, AuthConfig{ConsoleURL: input}); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateAuthConfig(EnvironmentProd, AuthConfig{ConsoleURL: "http://127.0.0.1:5173"}); err == nil {
		t.Fatal("validateAuthConfig() accepted http console_url for env prod")
	}
	if err := validateAuthConfig(EnvironmentDev, AuthConfig{ConsoleURL: "http://s1.localhost:5174"}); err != nil {
		t.Fatal(err)
	}
}
