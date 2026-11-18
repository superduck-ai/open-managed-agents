package vaults

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCredentialBindingsRespectScopeAndAttachmentOrder(t *testing.T) {
	shared := db.VaultCredential{ExternalID: "shared", VaultExternalID: "vlt_shared", CredentialKey: "TOKEN", AuthType: "env_oauth"}
	personal := db.VaultCredential{ExternalID: "personal", VaultExternalID: "vlt_a", CredentialKey: "TOKEN", AuthType: "environment_variable"}
	other := db.VaultCredential{ExternalID: "other", VaultExternalID: "vlt_b", CredentialKey: "OTHER", AuthType: "env_oauth"}
	credentials := []db.VaultCredential{shared, personal, other}
	for _, test := range []struct {
		ids  []string
		want string
	}{
		{[]string{"vlt_shared", "vlt_a"}, "shared"},
		{[]string{"vlt_a"}, "personal"},
		{[]string{"vlt_b"}, "other"},
	} {
		selected := SelectCredentialBindings(db.CredentialScope{VaultIDs: test.ids, PinBindings: true}, credentials)
		if len(selected) != 1 || selected[0].ExternalID != test.want {
			t.Fatalf("凭据范围或挂载顺序错误：%+v", selected)
		}
	}
}

func TestPinnedEnvironmentEgressDoesNotInjectOtherVaultPlaceholders(t *testing.T) {
	svc := newTestSecretsService(t)
	credential := sealedEnvCredential(t, svc, "TOKEN", "oma_ph_a", "service.example", "synthetic-a")
	store := &fakeCredentialStore{vaultIDs: []string{credential.VaultExternalID}, credentials: []db.VaultCredential{credential}, scope: db.CredentialScope{PinBindings: true}}
	egress := newEgressSubstitutor(store, svc, nil)
	for _, test := range []struct {
		placeholder string
		allowed     bool
	}{{"oma_ph_a", true}, {"oma_ph_other", false}} {
		request, err := http.NewRequest(http.MethodGet, "https://service.example/query", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+test.placeholder)
		err = egress.SubstituteEnvSecrets(t.Context(), "cse_test", credential.OrganizationUUID, credential.WorkspaceUUID, request, "service.example", "443")
		if err != nil {
			t.Fatal(err)
		}
		if !test.allowed && request.Header.Get("Authorization") != "Bearer "+test.placeholder {
			t.Fatal("范围外占位符被替换")
		}
		if test.allowed && request.Header.Get("Authorization") != "Bearer synthetic-a" {
			t.Fatal("未注入当前范围的凭据")
		}
	}
}

func TestPinnedMCPFailureCannotFallBackToAnotherVault(t *testing.T) {
	env := newOAuthRefreshEnv(t, "")
	credential := env.staleCred("revoked")
	store := &fakeCredentialStore{get: credential}
	injector := env.injector(store)
	_, err := injector.resolveFromPlan(t.Context(), injectionPlan{scope: db.CredentialScope{PinBindings: true, OAuthVaultID: "vlt_other"}, matches: []*db.VaultCredential{&credential}, hostCovered: true}, nil, nil)
	if !errors.Is(err, ErrCredentialAuthorizationManaged) || errors.Is(err, ErrMCPOAuthReauthorizationRequired) {
		t.Fatalf("失效的固定连接不应触发其他 Vault 的授权：%v", err)
	}
	if InjectionPublicMessage(err) != ErrCredentialAuthorizationManaged.Error() {
		t.Fatal("未向调用方说明原 Vault 需要重新授权")
	}
}
