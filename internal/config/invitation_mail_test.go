package config

import "testing"

func TestInvitationConsoleURL(t *testing.T) {
	for _, input := range []string{"http://oma.example", "https://user:password@oma.example", "https://oma.example/path", "https://oma.example?redirect=evil", "https://oma.example#hash", "//oma.example"} {
		if err := validateAuthConfig(AuthConfig{ConsoleURL: input}); err == nil {
			t.Errorf("接受了不安全的公开地址 %s", input)
		}
	}
	for _, input := range []string{"", "https://oma.example", "https://oma.example/"} {
		if err := validateAuthConfig(AuthConfig{ConsoleURL: input}); err != nil {
			t.Fatal(err)
		}
	}
}
