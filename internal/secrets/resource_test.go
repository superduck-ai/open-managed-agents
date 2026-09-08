package secrets_test

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestResourceEnvelopeBindsTenantAndDomain(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	binding := secrets.ResourceBinding{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	envelope, err := service.SealResource(ctx, binding, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*secrets.ResourceBinding){
		func(b *secrets.ResourceBinding) { b.OrganizationUUID = "other" },
		func(b *secrets.ResourceBinding) { b.WorkspaceUUID = "other" },
		func(b *secrets.ResourceBinding) { b.OrganizationUUID = "" },
		func(b *secrets.ResourceBinding) { b.WorkspaceUUID = "" },
	} {
		other := binding
		modify(&other)
		if _, err := service.OpenResource(ctx, other, envelope); err == nil {
			t.Fatal("modified binding accepted")
		}
	}
	vault := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "owner", CredentialExternalID: "resource"}
	if _, err := service.Open(ctx, vault, envelope); err == nil {
		t.Fatal("resource secret decrypts as vault credential")
	}
	old, err := service.Seal(ctx, vault, []byte("vault-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenResource(ctx, binding, old); err == nil {
		t.Fatal("vault credential decrypts as resource secret")
	}
	plaintext, err := service.OpenResource(ctx, binding, envelope)
	if err != nil || string(plaintext) != "secret" {
		t.Fatalf("resource decrypt = %v", err)
	}
}
