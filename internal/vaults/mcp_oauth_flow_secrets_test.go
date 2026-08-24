package vaults

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestClientSecretForMCPOAuthPersistDropsPlatformSecret(t *testing.T) {
	t.Parallel()

	if got := ClientSecretForMCPOAuthPersist(MCPOAuthClientCredentialPlatform, "platform-secret"); got != "" {
		t.Fatalf("platform persist secret = %q, want empty", got)
	}
	if got := ClientSecretForMCPOAuthPersist(MCPOAuthClientCredentialSealed, "byo-secret"); got != "byo-secret" {
		t.Fatalf("sealed persist secret = %q, want byo-secret", got)
	}
	if got := ClientSecretForMCPOAuthPersist("", "orphan-secret"); got != "" {
		t.Fatalf("unknown source persist secret = %q, want empty", got)
	}
}

func TestResolveMCPOAuthTokenClientSecret(t *testing.T) {
	t.Parallel()

	clients := []config.PlatformOAuthClientConfig{{
		MCPServerURL: "https://api.githubcopilot.com/mcp/",
		ClientID:     "platform-id",
		ClientSecret: "platform-secret",
	}}

	t.Run("failure platform missing", func(t *testing.T) {
		_, err := ResolveMCPOAuthTokenClientSecret(
			MCPOAuthClientCredentialPlatform,
			"https://missing.example/mcp",
			"",
			clients,
		)
		if !errors.Is(err, errMCPOAuthPlatformClientMissing) {
			t.Fatalf("error = %v, want errMCPOAuthPlatformClientMissing", err)
		}
	})

	t.Run("success platform empty secret allowed", func(t *testing.T) {
		got, err := ResolveMCPOAuthTokenClientSecret(
			MCPOAuthClientCredentialPlatform,
			"https://public.example/mcp",
			"ignored",
			[]config.PlatformOAuthClientConfig{{
				MCPServerURL: "https://public.example/mcp",
				ClientID:     "public-id",
			}},
		)
		if err != nil {
			t.Fatalf("ResolveMCPOAuthTokenClientSecret() error = %v", err)
		}
		if got != "" {
			t.Fatalf("secret = %q, want empty", got)
		}
	})

	t.Run("success platform from config", func(t *testing.T) {
		got, err := ResolveMCPOAuthTokenClientSecret(
			MCPOAuthClientCredentialPlatform,
			"https://api.githubcopilot.com/mcp/",
			"should-not-use-opened",
			clients,
		)
		if err != nil {
			t.Fatalf("ResolveMCPOAuthTokenClientSecret() error = %v", err)
		}
		if got != "platform-secret" {
			t.Fatalf("secret = %q, want platform-secret", got)
		}
	})

	t.Run("success sealed uses opened secret", func(t *testing.T) {
		got, err := ResolveMCPOAuthTokenClientSecret(
			MCPOAuthClientCredentialSealed,
			"https://api.githubcopilot.com/mcp/",
			"byo-secret",
			clients,
		)
		if err != nil {
			t.Fatalf("ResolveMCPOAuthTokenClientSecret() error = %v", err)
		}
		if got != "byo-secret" {
			t.Fatalf("secret = %q, want byo-secret", got)
		}
	})
}

func TestSealOpenMCPOAuthFlowSecretsRoundTrip(t *testing.T) {
	t.Parallel()

	svc := newVaultSecretsTestService(t)
	flow := db.MCPOAuthFlow{
		ExternalID:       "mcpoauth_test",
		OrganizationUUID: "org-1",
		WorkspaceUUID:    "ws-1",
		VaultExternalID:  "vlt_test",
	}

	t.Run("failure empty verifier", func(t *testing.T) {
		_, err := SealMCPOAuthFlowSecrets(context.Background(), svc, flow, "secret", "")
		if !errors.Is(err, errMCPOAuthFlowCodeVerifierRequired) {
			t.Fatalf("error = %v, want errMCPOAuthFlowCodeVerifierRequired", err)
		}
	})

	t.Run("success platform seals verifier only", func(t *testing.T) {
		platformFlow := flow
		platformFlow.ClientCredentialSource = MCPOAuthClientCredentialPlatform
		envelope, err := SealMCPOAuthFlowSecrets(
			context.Background(),
			svc,
			platformFlow,
			"platform-secret",
			"verifier-1",
		)
		if err != nil {
			t.Fatalf("SealMCPOAuthFlowSecrets() error = %v", err)
		}
		platformFlow.SecretEnvelope = &envelope
		secret, verifier, err := OpenMCPOAuthFlowSecrets(context.Background(), svc, platformFlow)
		if err != nil {
			t.Fatalf("OpenMCPOAuthFlowSecrets() error = %v", err)
		}
		if secret != "" {
			t.Fatalf("opened client_secret = %q, want empty for platform", secret)
		}
		if verifier != "verifier-1" {
			t.Fatalf("opened code_verifier = %q, want verifier-1", verifier)
		}
	})

	t.Run("success sealed keeps client secret", func(t *testing.T) {
		sealedFlow := flow
		sealedFlow.ClientCredentialSource = MCPOAuthClientCredentialSealed
		envelope, err := SealMCPOAuthFlowSecrets(context.Background(), svc, sealedFlow, "byo-secret", "verifier-2")
		if err != nil {
			t.Fatalf("SealMCPOAuthFlowSecrets() error = %v", err)
		}
		sealedFlow.SecretEnvelope = &envelope
		secret, verifier, err := OpenMCPOAuthFlowSecrets(context.Background(), svc, sealedFlow)
		if err != nil {
			t.Fatalf("OpenMCPOAuthFlowSecrets() error = %v", err)
		}
		if secret != "byo-secret" || verifier != "verifier-2" {
			t.Fatalf("opened = (%q, %q)", secret, verifier)
		}
	})

	t.Run("failure wrong binding", func(t *testing.T) {
		envelope, err := SealMCPOAuthFlowSecrets(context.Background(), svc, flow, "byo-secret", "verifier-3")
		if err != nil {
			t.Fatalf("SealMCPOAuthFlowSecrets() error = %v", err)
		}
		other := flow
		other.SecretEnvelope = &envelope
		other.WorkspaceUUID = "ws-other"
		_, _, err = OpenMCPOAuthFlowSecrets(context.Background(), svc, other)
		if err == nil {
			t.Fatal("OpenMCPOAuthFlowSecrets() error = nil, want AAD mismatch")
		}
	})
}

func TestSealMCPOAuthFlowSecretsRejectsNilService(t *testing.T) {
	t.Parallel()
	_, err := SealMCPOAuthFlowSecrets(context.Background(), nil, db.MCPOAuthFlow{
		ExternalID: "mcpoauth_x", OrganizationUUID: "o", WorkspaceUUID: "w", VaultExternalID: "v",
	}, "", "verifier")
	if !errors.Is(err, errMCPOAuthFlowSecretServiceRequired) {
		t.Fatalf("error = %v, want errMCPOAuthFlowSecretServiceRequired", err)
	}
}

func newVaultSecretsTestService(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := secrets.GenerateKEK()
	if err != nil {
		t.Fatalf("generate KEK: %v", err)
	}
	svc, err := secrets.NewLocalService(context.Background(), kek)
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	return svc
}
