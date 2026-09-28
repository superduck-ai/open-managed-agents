package local_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func TestLocalKeyProviderRejectsBadMaterial(t *testing.T) {
	if _, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: make([]byte, 16)}, nil); err == nil {
		t.Fatal("short KEK must fail")
	}
	kek, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("generate KEK: %v", err)
	}
	other, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("generate other KEK: %v", err)
	}
	if _, err := localkeys.New(
		localkeys.KeyMaterial{Version: 2, KEK: kek},
		[]localkeys.KeyMaterial{{Version: 2, KEK: other}},
	); err == nil {
		t.Fatal("decrypt_only colliding with current version must fail")
	}
}

func TestDecryptOnlyOpensOldKeyVersionWithoutRewrap(t *testing.T) {
	v1, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("generate v1 KEK: %v", err)
	}
	v2, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("generate v2 KEK: %v", err)
	}
	binding := secrets.Binding{OrganizationUUID: "org-1", WorkspaceUUID: "ws-2", VaultExternalID: "vlt_1", CredentialExternalID: "cred_1"}
	plaintext := []byte(`{"token":"rotate-me"}`)

	oldSvcProvider, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: v1}, nil)
	if err != nil {
		t.Fatalf("build v1 service: %v", err)
	}
	oldSvc := secrets.NewService(oldSvcProvider)
	env := mustSeal(t, oldSvc, binding, plaintext)

	rotatedProvider, err := localkeys.New(localkeys.KeyMaterial{Version: 2, KEK: v2}, []localkeys.KeyMaterial{{Version: 1, KEK: v1}})
	if err != nil {
		t.Fatalf("build rotated service: %v", err)
	}
	rotated := secrets.NewService(rotatedProvider)
	got, err := rotated.Open(context.Background(), binding, env)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatalf("Open old envelope after rotation: %v got %q", err, got)
	}
	if fresh := mustSeal(t, rotated, binding, []byte("new")); fresh.KeyVersion != 2 {
		t.Fatalf("fresh seal key_version = %d, want 2", fresh.KeyVersion)
	}

	currentOnlyProvider, err := localkeys.New(localkeys.KeyMaterial{Version: 2, KEK: v2}, nil)
	if err != nil {
		t.Fatalf("build current-only service: %v", err)
	}
	currentOnly := secrets.NewService(currentOnlyProvider)
	if _, err := currentOnly.Open(context.Background(), binding, env); err == nil {
		t.Fatal("Open old envelope without decrypt_only succeeded")
	}
}

func mustSeal(t *testing.T, svc *secrets.Service, binding secrets.Binding, plaintext []byte) secrets.Envelope {
	t.Helper()
	env, err := svc.Seal(t.Context(), binding, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// This fixture was sealed by the implementation before the local package split.
// Its KEK and plaintext are fixed public test data.
func TestLegacyEnvelopeCompatibility(t *testing.T) {
	const legacyEnvelope = `{
  "ciphertext": "KfGORJFpdfb5xbLGkd9kkGt3B/25lUqQxRmBwV5H2QAl/+0=",
  "nonce": "m9OAflnNpAjuVg4W",
  "wrapped_dek": "EV3ICz8F8H2W737fccA2InDJkdzVb87l+sXTHDCvTpQE5fRCr82Wa2Xm74iio8EtElDRgalHiHtxH8UV",
  "format_version": 1,
  "key_provider": "local",
  "key_version": 1
}`
	var env secrets.Envelope
	if err := json.Unmarshal([]byte(legacyEnvelope), &env); err != nil {
		t.Fatal(err)
	}
	p, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{3}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := secrets.NewService(p)
	plain, err := svc.Open(t.Context(), secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vault", CredentialExternalID: "credential"}, env)
	defer clear(plain)
	if err != nil || string(plain) != "legacy-local-secret" {
		t.Fatalf("legacy envelope unreadable: %v", err)
	}
}
