package config

import "testing"

func TestOAuthServiceBindings(t *testing.T) {
	for _, bindings := range [][]OAuthServiceBindingConfig{
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "relative"}},
		{{ServiceURL: "https://user:secret@bridge.example", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example?", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example#", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example?q=1"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example?"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example#"}},
		{{ServiceURL: "https://bridge.example:0", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example:65536", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example:99999999999999999999", CredentialTargetURL: "https://api.example"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example:0/mcp"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example:65536/mcp"}},
		{{ServiceURL: "https://bridge.example", CredentialTargetURL: "https://api.example:99999999999999999999/mcp"}},
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
	bindings = []OAuthServiceBindingConfig{{ServiceURL: "https://bridge.example/%3F", CredentialTargetURL: "https://api.example/%23"}}
	if err := validateOAuthServiceBindings(bindings); err != nil {
		t.Fatal(err)
	}
	if got := OAuthCredentialTargetURL(bindings, "https://bridge.example/%3F"); got != "https://api.example/%23" {
		t.Fatalf("encoded target = %s", got)
	}
	for _, port := range []string{"1", "65535"} {
		bindings = []OAuthServiceBindingConfig{{ServiceURL: "http://127.0.0.1:" + port, CredentialTargetURL: "https://[::1]:" + port + "/mcp"}}
		if err := validateOAuthServiceBindings(bindings); err != nil {
			t.Fatalf("rejected valid port %s: %v", port, err)
		}
	}
}
