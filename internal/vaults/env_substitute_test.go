package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestSubstituteEnvSecretsRejectsNilStore(t *testing.T) {
	t.Parallel()
	req, err := http.NewRequest(http.MethodGet, "https://api.notion.com/v1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	err = newEgressSubstitutor(nil, nil, nil).SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		req,
		"api.notion.com",
		"443",
	)
	if !errors.Is(err, ErrSubstitutionRejected) {
		t.Fatalf("error = %v, want ErrSubstitutionRejected", err)
	}
}

func TestSubstituteEnvSecretsBodyLocationRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvCredentialWithLocation(t, svc, "NOTION_API_KEY", "oma_ph_a", "api.notion.com", "ntn_real", false, true)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(`{"token":"oma_ph_a"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.ContentLength = maxSnapshotRequestBodyBytes + 1

	err = substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	)
	if !errors.Is(err, ErrSubstitutionRejected) {
		t.Fatalf("error = %v, want ErrSubstitutionRejected", err)
	}
	if !errors.Is(err, errSnapshotRequestBodyTooLarge) {
		t.Fatalf("error = %v, want errSnapshotRequestBodyTooLarge", err)
	}
	if !strings.Contains(err.Error(), "environment variable body substitution") {
		t.Fatalf("error = %v, want body substitution cause", err)
	}
	if got := SubstitutionPublicMessage(err); got != SubstitutionBodyTooLargePublicMessage {
		t.Fatalf("public message = %q", got)
	}
}

func TestSubstituteEnvSecretsHeaderAndBodyRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvCredentialWithLocation(t, svc, "NOTION_API_KEY", "oma_ph_a", "api.notion.com", "ntn_real", true, true)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader("oma_ph_a"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")
	req.ContentLength = maxSnapshotRequestBodyBytes + 1

	err = substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	)
	if !errors.Is(err, ErrSubstitutionRejected) || !errors.Is(err, errSnapshotRequestBodyTooLarge) {
		t.Fatalf("error = %v, want substitution rejected for oversized body", err)
	}
}

func TestSubstituteEnvSecretsHeaderOnlyIgnoresBodyPlaceholder(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvCredentialWithLocation(t, svc, "NOTION_API_KEY", "oma_ph_a", "api.notion.com", "ntn_real", true, false)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(`{"token":"oma_ph_a"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ntn_real" {
		t.Fatalf("Authorization = %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"token":"oma_ph_a"}` {
		t.Fatalf("body = %s, want unsubstituted placeholder", body)
	}
}

func TestSubstituteEnvSecretsBodyOnlyIgnoresHeaderPlaceholder(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvCredentialWithLocation(t, svc, "NOTION_API_KEY", "oma_ph_a", "api.notion.com", "ntn_real", false, true)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(`{"token":"oma_ph_a"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer oma_ph_a" {
		t.Fatalf("Authorization = %q, want unsubstituted placeholder", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"token":"ntn_real"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestSubstituteEnvSecretsHeaderOnlyAllowsOversizedContentLength(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	credential := sealedEnvCredentialWithLocation(t, svc, "NOTION_API_KEY", "oma_ph_a", "api.notion.com", "ntn_real", true, false)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader("not-a-placeholder"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")
	req.ContentLength = maxSnapshotRequestBodyBytes + 1

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ntn_real" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestSubstituteEnvSecretsHeaderAndBody(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	auth, _ := json.Marshal(map[string]any{
		"type":               "environment_variable",
		"secret_name":        "NOTION_API_KEY",
		"placeholder":        "oma_ph_a",
		"networking":         map[string]any{"type": "limited", "allowed_hosts": []string{"api.notion.com"}},
		"injection_location": map[string]any{"header": true, "body": true},
	})
	secret, _ := json.Marshal(map[string]any{"type": "environment_variable", "secret_value": "ntn_real"})
	credential := db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_a",
		ExternalID:       "cred_a",
		AuthType:         "environment_variable",
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(`{"token":"oma_ph_a"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ntn_real" {
		t.Fatalf("Authorization = %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"token":"ntn_real"}` {
		t.Fatalf("body = %s", body)
	}
}

func TestSubstituteEnvSecretsDoesNotCascadeWhenSecretEqualsOtherPlaceholder(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	const (
		placeholderA = "oma_ph_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		placeholderB = "oma_ph_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		secretB      = "secret_b"
	)
	first := sealedEnvCredentialWithLocation(t, svc, "TOKEN_A", placeholderA, "api.notion.com", placeholderB, true, true)
	second := sealedEnvCredentialWithLocation(t, svc, "TOKEN_B", placeholderB, "api.notion.com", secretB, true, true)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a", "vlt_b"},
		credentials: []db.VaultCredential{first, second},
	}, svc, nil)
	original := placeholderA + "|" + placeholderB
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(original))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", original)

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		first.OrganizationUUID,
		first.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	want := placeholderB + "|" + secretB
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestSubstituteEnvSecretsFirstSecretNameWins(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	first := sealedEnvCredential(t, svc, "SHARED", "oma_ph_first", "other.example.com", "secret_first")
	second := sealedEnvCredential(t, svc, "SHARED", "oma_ph_second", "api.notion.com", "secret_second")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{"vlt_a", "vlt_b"},
		credentials: []db.VaultCredential{first, second},
	}, svc, nil)
	req, err := http.NewRequest(http.MethodGet, "https://api.notion.com/v1", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-First", "oma_ph_first")
	req.Header.Set("X-Second", "oma_ph_second")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		first.OrganizationUUID,
		first.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("X-First"); got != "oma_ph_first" {
		t.Fatalf("first placeholder = %q, want passthrough", got)
	}
	if got := req.Header.Get("X-Second"); got != "oma_ph_second" {
		t.Fatalf("second placeholder = %q, want ignored after first-wins", got)
	}
}

func sealedEnvCredential(t *testing.T, svc *secrets.Service, secretName, placeholder, host, secretValue string) db.VaultCredential {
	t.Helper()
	return sealedEnvCredentialWithLocation(t, svc, secretName, placeholder, host, secretValue, true, false)
}

func sealedEnvCredentialWithLocation(t *testing.T, svc *secrets.Service, secretName, placeholder, host, secretValue string, header, body bool) db.VaultCredential {
	t.Helper()
	auth, _ := json.Marshal(map[string]any{
		"type":               "environment_variable",
		"secret_name":        secretName,
		"placeholder":        placeholder,
		"networking":         map[string]any{"type": "limited", "allowed_hosts": []string{host}},
		"injection_location": map[string]any{"header": header, "body": body},
	})
	secret, _ := json.Marshal(map[string]any{"type": "environment_variable", "secret_value": secretValue})
	credential := db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_" + secretName,
		ExternalID:       "cred_" + placeholder,
		AuthType:         "environment_variable",
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	return credential
}
