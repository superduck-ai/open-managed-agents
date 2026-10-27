package secrets_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func newTestService(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("generate KEK: %v", err)
	}
	svcProvider, err := localkeys.New(localkeys.KeyMaterial{KEK: kek}, nil)
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	svc := secrets.NewService(svcProvider)
	return svc
}

func mustSeal(t *testing.T, svc *secrets.Service, binding secrets.Binding, plaintext []byte) secrets.Envelope {
	t.Helper()
	env, err := svc.Seal(context.Background(), binding, plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return env
}

func TestSealRejectsIncompleteBinding(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Seal(context.Background(), secrets.Binding{
		WorkspaceUUID: "ws-2", VaultExternalID: "vlt_1", CredentialExternalID: "cred_1",
	}, []byte(`{"token":"x"}`))
	if !errors.Is(err, secrets.ErrIncompleteBinding) {
		t.Fatalf("Seal() error = %v, want ErrIncompleteBinding", err)
	}
}

func TestOpenTamperAndAADFailClosed(t *testing.T) {
	svc := newTestService(t)
	binding := secrets.Binding{OrganizationUUID: "org-1", WorkspaceUUID: "ws-2", VaultExternalID: "vlt_1", CredentialExternalID: "cred_1"}
	env := mustSeal(t, svc, binding, []byte("secret"))

	tampered := env
	tampered.Ciphertext = append([]byte(nil), env.Ciphertext...)
	tampered.Ciphertext[0] ^= 0x01
	if _, err := svc.Open(context.Background(), binding, tampered); err == nil {
		t.Fatal("Open with tampered ciphertext succeeded")
	}

	other := binding
	other.WorkspaceUUID = "ws-other"
	if _, err := svc.Open(context.Background(), other, env); err == nil {
		t.Fatal("Open with mismatched AAD succeeded")
	}

	badMeta := env
	badMeta.FormatVersion = 999
	if _, err := svc.Open(context.Background(), binding, badMeta); !errors.Is(err, secrets.ErrUnknownEnvelopeFormat) {
		t.Fatalf("Open unknown format = %v, want ErrUnknownEnvelopeFormat", err)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	svc := newTestService(t)
	binding := secrets.Binding{OrganizationUUID: "org-1", WorkspaceUUID: "ws-2", VaultExternalID: "vlt_1", CredentialExternalID: "cred_1"}
	plaintext := []byte(`{"type":"static_bearer","token":"hunter2"}`)
	env := mustSeal(t, svc, binding, plaintext)
	got, err := svc.Open(context.Background(), binding, env)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext mismatch: got %q want %q", got, plaintext)
	}
	again := mustSeal(t, svc, binding, plaintext)
	if bytes.Equal(env.Nonce, again.Nonce) || bytes.Equal(env.WrappedDEK, again.WrappedDEK) {
		t.Fatal("nonce and wrapped DEK must differ between seals")
	}
}

func TestTunnelEnvelopeBindsEveryIdentityField(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	binding := secrets.TunnelBinding{
		OrganizationUUID: "11111111-1111-1111-1111-111111111111",
		WorkspaceUUID:    "22222222-2222-2222-2222-222222222222",
		TunnelExternalID: "tunnel_0123456789abcdef0123456789abcdef",
		TokenExternalID:  "ttkn_example",
	}
	envelope, err := svc.SealTunnel(ctx, binding, []byte("connector-secret"))
	if err != nil {
		t.Fatalf("SealTunnel: %v", err)
	}
	mutations := []struct {
		name    string
		binding secrets.TunnelBinding
	}{
		{name: "organization", binding: secrets.TunnelBinding{OrganizationUUID: "other", WorkspaceUUID: binding.WorkspaceUUID, TunnelExternalID: binding.TunnelExternalID, TokenExternalID: binding.TokenExternalID}},
		{name: "workspace", binding: secrets.TunnelBinding{OrganizationUUID: binding.OrganizationUUID, WorkspaceUUID: "other", TunnelExternalID: binding.TunnelExternalID, TokenExternalID: binding.TokenExternalID}},
		{name: "tunnel", binding: secrets.TunnelBinding{OrganizationUUID: binding.OrganizationUUID, WorkspaceUUID: binding.WorkspaceUUID, TunnelExternalID: "tunnel_fedcba9876543210fedcba9876543210", TokenExternalID: binding.TokenExternalID}},
		{name: "token", binding: secrets.TunnelBinding{OrganizationUUID: binding.OrganizationUUID, WorkspaceUUID: binding.WorkspaceUUID, TunnelExternalID: binding.TunnelExternalID, TokenExternalID: "ttkn_other"}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if _, err := svc.OpenTunnel(ctx, mutation.binding, envelope); err == nil {
				t.Fatal("OpenTunnel succeeded with a changed binding")
			}
		})
	}

	plaintext, err := svc.OpenTunnel(ctx, binding, envelope)
	if err != nil {
		t.Fatalf("OpenTunnel: %v", err)
	}
	if got, want := string(plaintext), "connector-secret"; got != want {
		t.Fatalf("plaintext = %q, want %q", got, want)
	}
}

// observedProvider retains the DEK slice only so the test can inspect its cleanup.
type observedProvider struct {
	dek  []byte
	fail bool
}

func (*observedProvider) Name() string { return "observed" }
func (p *observedProvider) WrapDEK(_ context.Context, dek []byte) (secrets.WrappedKey, error) {
	p.dek = dek
	return secrets.WrappedKey{Ciphertext: bytes.Clone(dek), KeyVersion: 1}, nil
}
func (p *observedProvider) UnwrapDEK(_ context.Context, w secrets.WrappedKey) ([]byte, error) {
	p.dek = bytes.Clone(w.Ciphertext)
	if p.fail {
		return p.dek, errors.New("unwrap failure")
	}
	return p.dek, nil
}

func TestServiceWipesDEK(t *testing.T) {
	for _, fail := range []bool{true, false} {
		p := &observedProvider{fail: fail}
		svc := secrets.NewService(p)
		b := secrets.Binding{OrganizationUUID: "o", WorkspaceUUID: "w", VaultExternalID: "v", CredentialExternalID: "c"}
		env := mustSeal(t, svc, b, []byte("secret"))
		if !bytes.Equal(p.dek, make([]byte, 32)) {
			t.Fatal("Seal retained a plaintext DEK")
		}
		plain, err := svc.Open(t.Context(), b, env)
		if fail {
			if err == nil || plain != nil {
				t.Fatal("unwrap failure returned plaintext")
			}
		} else if err != nil || string(plain) != "secret" {
			t.Fatalf("roundtrip failed: %v", err)
		}
		if !bytes.Equal(p.dek, make([]byte, 32)) {
			t.Fatalf("Open retained a plaintext DEK (provider failure: %t)", fail)
		}
	}
}
