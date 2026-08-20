package vaults

import (
	"context"
	"encoding/base64"
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
	err = NewEgressSubstitutor(nil, nil, nil).SubstituteEnvSecrets(
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

func TestSubstituteEnvSecretsGitSmartHTTPRejectsOpenFailure(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	cred.SecretEnvelope = nil
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack")

	err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		cred.OrganizationUUID,
		cred.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	)
	if !errors.Is(err, ErrSubstitutionRejected) {
		t.Fatalf("error = %v, want ErrSubstitutionRejected", err)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPPassthroughWhenHostUncovered(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://github.com/org/repo.git/info/refs?service=git-upload-pack")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		cred.OrganizationUUID,
		cred.WorkspaceUUID,
		req,
		"github.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want passthrough", got)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPSkipsNonGit(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}, svc, nil)

	cases := []string{
		"https://gitlab.example.com/api/v4/user",
		"https://gitlab.example.com/group/repo.git/info/lfs/objects/batch",
		"https://gitlab.example.com/group/repo.git/info/refs",
	}
	for _, rawURL := range cases {
		req := mustHTTPRequest(t, http.MethodGet, rawURL)
		if err := substitutor.SubstituteEnvSecrets(
			context.Background(),
			"cse_test",
			cred.OrganizationUUID,
			cred.WorkspaceUUID,
			req,
			"gitlab.example.com",
			"443",
		); err != nil {
			t.Fatalf("%s: %v", rawURL, err)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("%s Authorization = %q, want empty", rawURL, got)
		}
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPRequiresHeaderLocation(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	bodyOnly := sealedEnvCredentialAt(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real", false, true)
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{bodyOnly.VaultExternalID},
		credentials: []db.VaultCredential{bodyOnly},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		bodyOnly.OrganizationUUID,
		bodyOnly.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty for body-only credential", got)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPWritesBasic(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		cred.OrganizationUUID,
		cred.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("glpat-real") {
		t.Fatalf("Authorization = %q, want oauth2 Basic", got)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPFirstCoveringWins(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	first := sealedEnvCredential(t, svc, "NOTION_TOKEN", "oma_ph_notion", "gitlab.example.com", "ntn_wrong")
	second := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{first.VaultExternalID, second.VaultExternalID},
		credentials: []db.VaultCredential{first, second},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodPost, "https://gitlab.example.com/group/repo.git/git-receive-pack")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		first.OrganizationUUID,
		first.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("ntn_wrong") {
		t.Fatalf("Authorization = %q, want first covering secret", got)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPSameSecretNameLaterVaultCoversHost(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	first := sealedEnvCredential(t, svc, "SHARED", "oma_ph_first", "other.example.com", "secret_other")
	second := sealedEnvCredential(t, svc, "SHARED", "oma_ph_second", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{first.VaultExternalID, second.VaultExternalID},
		credentials: []db.VaultCredential{first, second},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		first.OrganizationUUID,
		first.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("glpat-real") {
		t.Fatalf("Authorization = %q, want later vault covering this host", got)
	}
}

func TestSubstituteEnvSecretsGitSmartHTTPOverwritesAuthorizationAndKeepsSubstitution(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	substitutor := newEgressSubstitutor(&fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}, svc, nil)
	req := mustHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/git-upload-pack")
	req.Header.Set("Authorization", "Bearer oma_ph_git")
	req.Header.Set("PRIVATE-TOKEN", "oma_ph_git")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		cred.OrganizationUUID,
		cred.WorkspaceUUID,
		req,
		"gitlab.example.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("glpat-real") {
		t.Fatalf("Authorization = %q, want overwritten Basic", got)
	}
	if got := req.Header.Get("PRIVATE-TOKEN"); got != "glpat-real" {
		t.Fatalf("PRIVATE-TOKEN = %q, want substituted secret", got)
	}
}

func oauth2Basic(secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:"+secret))
}

func mustHTTPRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func sealedEnvCredential(t *testing.T, svc *secrets.Service, secretName, placeholder, host, secretValue string) db.VaultCredential {
	t.Helper()
	return sealedEnvCredentialAt(t, svc, secretName, placeholder, host, secretValue, true, false)
}

func sealedEnvCredentialAt(t *testing.T, svc *secrets.Service, secretName, placeholder, host, secretValue string, header, body bool) db.VaultCredential {
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
