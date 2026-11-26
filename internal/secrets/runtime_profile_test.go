package secrets_test

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestRuntimeProfileEnvelopeBindsScopeOrgProfileAndRevision(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	orgBinding := secrets.RuntimeProfileBinding{
		Scope:              "organization",
		OrganizationUUID:   "11111111-1111-4111-8111-111111111111",
		RuntimeProfileUUID: "22222222-2222-4222-8222-222222222222",
		CredentialRevision: 1,
	}
	envelope, err := service.SealRuntimeProfile(ctx, orgBinding, []byte(`{"api_key":"sk-secret"}`))
	if err != nil {
		t.Fatalf("SealRuntimeProfile(org): %v", err)
	}

	plaintext, err := service.OpenRuntimeProfile(ctx, orgBinding, envelope)
	if err != nil || string(plaintext) != `{"api_key":"sk-secret"}` {
		t.Fatalf("OpenRuntimeProfile(org) = (%q, %v)", string(plaintext), err)
	}

	for _, tc := range []struct {
		name   string
		modify func(*secrets.RuntimeProfileBinding)
	}{
		{
			name:   "cross organization",
			modify: func(b *secrets.RuntimeProfileBinding) { b.OrganizationUUID = "99999999-9999-4999-8999-999999999999" },
		},
		{
			name:   "cross profile",
			modify: func(b *secrets.RuntimeProfileBinding) { b.RuntimeProfileUUID = "33333333-3333-4333-8333-333333333333" },
		},
		{
			name:   "cross revision",
			modify: func(b *secrets.RuntimeProfileBinding) { b.CredentialRevision = 2 },
		},
		{
			name: "cross scope",
			modify: func(b *secrets.RuntimeProfileBinding) {
				b.Scope = "platform"
				b.OrganizationUUID = ""
			},
		},
		{
			name:   "missing profile uuid",
			modify: func(b *secrets.RuntimeProfileBinding) { b.RuntimeProfileUUID = "" },
		},
		{
			name:   "invalid revision",
			modify: func(b *secrets.RuntimeProfileBinding) { b.CredentialRevision = 0 },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tampered := orgBinding
			tc.modify(&tampered)
			if _, err := service.OpenRuntimeProfile(ctx, tampered, envelope); err == nil {
				t.Fatalf("OpenRuntimeProfile unexpectedly succeeded for %s", tc.name)
			}
		})
	}
}

func TestRuntimeProfilePlatformScopeAndCrossDomainIsolation(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	platformBinding := secrets.RuntimeProfileBinding{
		Scope:              "platform",
		RuntimeProfileUUID: "22222222-2222-4222-8222-222222222222",
		CredentialRevision: 3,
	}
	envelope, err := service.SealRuntimeProfile(ctx, platformBinding, []byte("platform-key"))
	if err != nil {
		t.Fatalf("SealRuntimeProfile(platform): %v", err)
	}
	got, err := service.OpenRuntimeProfile(ctx, platformBinding, envelope)
	if err != nil || string(got) != "platform-key" {
		t.Fatalf("OpenRuntimeProfile(platform) = (%q, %v)", string(got), err)
	}

	invalidBindings := []secrets.RuntimeProfileBinding{
		{Scope: "platform", OrganizationUUID: "org-1", RuntimeProfileUUID: "p-1", CredentialRevision: 1},
		{Scope: "organization", RuntimeProfileUUID: "p-1", CredentialRevision: 1},
		{Scope: "unknown", RuntimeProfileUUID: "p-1", CredentialRevision: 1},
		{Scope: "platform", RuntimeProfileUUID: "", CredentialRevision: 1},
		{Scope: "platform", RuntimeProfileUUID: "p-1", CredentialRevision: 0},
	}
	for _, invalid := range invalidBindings {
		if _, err := service.SealRuntimeProfile(ctx, invalid, []byte("x")); !errors.Is(err, secrets.ErrIncompleteBinding) {
			t.Fatalf("SealRuntimeProfile(%+v) err = %v, want ErrIncompleteBinding", invalid, err)
		}
	}

	resourceBinding := secrets.ResourceBinding{OrganizationUUID: "platform", WorkspaceUUID: platformBinding.RuntimeProfileUUID}
	if _, err := service.OpenResource(ctx, resourceBinding, envelope); err == nil {
		t.Fatal("runtime profile envelope decrypted as resource secret")
	}
	tunnelBinding := secrets.TunnelBinding{
		OrganizationUUID: "platform",
		WorkspaceUUID:    platformBinding.RuntimeProfileUUID,
		TunnelExternalID: "tun",
		TokenExternalID:  "tok",
	}
	if _, err := service.OpenTunnel(ctx, tunnelBinding, envelope); err == nil {
		t.Fatal("runtime profile envelope decrypted as tunnel secret")
	}
	vaultBinding := secrets.Binding{
		OrganizationUUID:     "platform",
		WorkspaceUUID:        platformBinding.RuntimeProfileUUID,
		VaultExternalID:      "vlt",
		CredentialExternalID: "cred",
	}
	if _, err := service.Open(ctx, vaultBinding, envelope); err == nil {
		t.Fatal("runtime profile envelope decrypted as vault secret")
	}
}
