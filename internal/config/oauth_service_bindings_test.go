package config

import "testing"

func TestOAuthServiceBindings(t *testing.T) {
	for _, bindings := range [][]OAuthServiceBindingConfig{
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "relative"}},
		{{ServiceURL: "https://user:secret@bridge.example", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example?q=1"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example"}, {ServiceURL: " https://bridge.example ", CredentialTargetURL: "https://other.example"}},
	} {
		if err := validateOAuthServiceBindings(bindings); err == nil {
			t.Fatalf("accepted invalid bindings")
		}
	}
	bindings := []OAuthServiceBindingConfig{{ServiceURL: " https://bridge.example ", CredentialTargetURL: " https://open.feishu.cn/open-apis "}}
	if err := validateOAuthServiceBindings(bindings); err != nil {
		t.Fatal(err)
	}
	if got := OAuthCredentialTargetURL(bindings, "https://bridge.example"); got != "https://open.feishu.cn/open-apis" {
		t.Fatalf("target = %s", got)
	}
	if got := OAuthCredentialTargetURL(bindings, "https://ordinary.example/mcp"); got != "https://ordinary.example/mcp" {
		t.Fatalf("unbound target = %s", got)
	}
}
