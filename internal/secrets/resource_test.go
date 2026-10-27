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

// Shared encryption helpers must retain independent AAD domains after merging
// resource credentials with MCP tunnel connector tokens.
func TestResourceAndTunnelEnvelopesRemainIsolated(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()
	resource := secrets.ResourceBinding{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	tunnel := secrets.TunnelBinding{
		OrganizationUUID: "org", WorkspaceUUID: "ws",
		TunnelExternalID: "tunnel", TokenExternalID: "token",
	}
	resourceEnvelope, err := service.SealResource(ctx, resource, []byte("resource-secret"))
	if err != nil {
		t.Fatal(err)
	}
	tunnelEnvelope, err := service.SealTunnel(ctx, tunnel, []byte("tunnel-secret"))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("resource cannot decrypt as tunnel", func(t *testing.T) {
		if _, err := service.OpenTunnel(ctx, tunnel, resourceEnvelope); err == nil {
			t.Fatal("resource secret decrypts as tunnel connector token")
		}
	})
	t.Run("tunnel cannot decrypt as resource", func(t *testing.T) {
		if _, err := service.OpenResource(ctx, resource, tunnelEnvelope); err == nil {
			t.Fatal("tunnel connector token decrypts as resource secret")
		}
	})
	t.Run("resource round trip", func(t *testing.T) {
		plaintext, err := service.OpenResource(ctx, resource, resourceEnvelope)
		if err != nil || string(plaintext) != "resource-secret" {
			t.Fatalf("resource decrypt failed: %v", err)
		}
	})
	t.Run("tunnel round trip", func(t *testing.T) {
		plaintext, err := service.OpenTunnel(ctx, tunnel, tunnelEnvelope)
		if err != nil || string(plaintext) != "tunnel-secret" {
			t.Fatalf("tunnel decrypt failed: %v", err)
		}
	})
}
