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
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestPrepareSessionVaultMountAllowsMCPCredentialsWithoutMITM(t *testing.T) {
	t.Parallel()

	cred := db.VaultCredential{
		AuthType: "static_bearer",
		Auth:     json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/mcp"}`),
	}
	// MITM off: Session still mounts; MCP injection stays on the explicit /mcp path.
	got, err := PrepareEnvCredentialMount(false, []db.VaultCredential{cred})
	if err != nil {
		t.Fatalf("MITM off should allow MCP credentials: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("placeholders = %#v, want empty (MCP creds do not provision env placeholders)", got)
	}

	_, err = PrepareEnvCredentialMount(true, []db.VaultCredential{cred})
	if err != nil {
		t.Fatalf("MITM on should allow MCP credentials: %v", err)
	}
}

type recordingTransport struct {
	lastAuth string
	calls    int
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	t.lastAuth = req.Header.Get("Authorization")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestMITMEgressPrepareInjectsBearerAndStripsClientAuthorization(t *testing.T) {
	t.Parallel()

	svc := newTestSecretsService(t)
	cred := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "vault-token", "cred_a")
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{cred},
	}
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	egress := newMITMEgressForTest(nil, injector)
	recorder := &recordingTransport{}

	req := newHTTPRequest(t, http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer ")

	tripper, err := egress.Prepare(
		context.Background(),
		EgressSession{
			CodeSessionExternalID: "cse_test",
			OrganizationUUID:      testInjectOrgUUID,
			WorkspaceUUID:         testInjectWsUUID,
		},
		"mcp.example.com:443",
		req,
		recorder,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	outReq := newHTTPRequest(t, http.MethodPost, "https://mcp.example.com/mcp", strings.NewReader(`{}`))
	outReq.Header.Set("Authorization", "Bearer ")
	resp, err := tripper.RoundTrip(outReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if recorder.lastAuth != "Bearer vault-token" {
		t.Fatalf("Authorization = %q, want vault bearer", recorder.lastAuth)
	}
}

func TestMITMEgressPrepareEnvThenInjectOrder(t *testing.T) {
	t.Parallel()

	svc := newTestSecretsService(t)
	mcpCred := sealedStaticBearerCredential(t, svc, "https://mcp.example.com/mcp", "vault-token", "cred_mcp")
	envCred := sealedEnvCredentialWithLocation(t, svc, "API_KEY", "oma_ph_key", "mcp.example.com", "real-secret", true, false)
	store := &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{mcpCred, envCred},
	}
	injector := newTestInjector(t, svc, store, nil, time.Time{})
	egress := newMITMEgressForTest(newEgressSubstitutor(store, svc, nil), injector)
	recorder := &recordingTransport{}

	req := newHTTPRequest(t, http.MethodPost, "/mcp", nil)
	req.Header.Set("X-Api-Key", "oma_ph_key")
	req.Header.Set("Authorization", "Bearer broken")

	tripper, err := egress.Prepare(
		context.Background(),
		EgressSession{
			CodeSessionExternalID: "cse_test",
			OrganizationUUID:      testInjectOrgUUID,
			WorkspaceUUID:         testInjectWsUUID,
		},
		"mcp.example.com:443",
		req,
		recorder,
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if req.Header.Get("X-Api-Key") != "real-secret" {
		t.Fatalf("env substitute before inject: X-Api-Key = %q", req.Header.Get("X-Api-Key"))
	}

	outReq := newHTTPRequest(t, http.MethodPost, "https://mcp.example.com/mcp", nil)
	outReq.Header.Set("X-Api-Key", req.Header.Get("X-Api-Key"))
	outReq.Header.Set("Authorization", "Bearer broken")
	resp, err := tripper.RoundTrip(outReq)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if recorder.lastAuth != "Bearer vault-token" {
		t.Fatalf("Authorization = %q", recorder.lastAuth)
	}
}

func newHTTPRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

func prepareMITMEgress(t *testing.T, store *fakeCredentialStore, svc *secrets.Service, authority string, req *http.Request) error {
	t.Helper()
	egress := newMITMEgressForTest(newEgressSubstitutor(store, svc, nil), nil)
	_, err := egress.Prepare(
		context.Background(),
		EgressSession{
			CodeSessionExternalID: "cse_test",
			OrganizationUUID:      testInjectOrgUUID,
			WorkspaceUUID:         testInjectWsUUID,
		},
		authority,
		req,
		http.DefaultTransport,
	)
	return err
}

func TestMITMEgressPrepareGitSmartHTTPRejectsOpenFailure(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	cred.SecretEnvelope = nil
	store := &fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", nil)
	err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req)
	if !errors.Is(err, ErrSubstitutionRejected) {
		t.Fatalf("error = %v, want ErrSubstitutionRejected", err)
	}
}

func TestMITMEgressPrepareGitSmartHTTPPassthroughWhenHostUncovered(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://github.com/org/repo.git/info/refs?service=git-upload-pack", nil)
	if err := prepareMITMEgress(t, store, svc, "github.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want passthrough", got)
	}
}

func TestMITMEgressPrepareGitSmartHTTPSkipsNonGit(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}
	cases := []string{
		"https://gitlab.example.com/api/v4/user",
		"https://gitlab.example.com/group/repo.git/info/lfs/objects/batch",
		"https://gitlab.example.com/group/repo.git/info/refs",
	}
	for _, rawURL := range cases {
		req := newHTTPRequest(t, http.MethodGet, rawURL, nil)
		if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
			t.Fatalf("%s: %v", rawURL, err)
		}
		if got := req.Header.Get("Authorization"); got != "" {
			t.Fatalf("%s Authorization = %q, want empty", rawURL, got)
		}
	}
}

func TestMITMEgressPrepareGitSmartHTTPRequiresHeaderLocation(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	bodyOnly := sealedEnvCredentialWithLocation(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real", false, true)
	store := &fakeCredentialStore{
		vaultIDs:    []string{bodyOnly.VaultExternalID},
		credentials: []db.VaultCredential{bodyOnly},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", nil)
	if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty for body-only credential", got)
	}
}

func TestMITMEgressPrepareGitSmartHTTPWritesBasic(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", nil)
	if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("glpat-real") {
		t.Fatalf("Authorization = %q, want oauth2 Basic", got)
	}
}

func TestMITMEgressPrepareGitSmartHTTPFirstCoveringWins(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	first := sealedEnvCredential(t, svc, "NOTION_TOKEN", "oma_ph_notion", "gitlab.example.com", "ntn_wrong")
	second := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{first.VaultExternalID, second.VaultExternalID},
		credentials: []db.VaultCredential{first, second},
	}
	req := newHTTPRequest(t, http.MethodPost, "https://gitlab.example.com/group/repo.git/git-receive-pack", nil)
	if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("ntn_wrong") {
		t.Fatalf("Authorization = %q, want first covering secret", got)
	}
}

func TestMITMEgressPrepareGitSmartHTTPSameSecretNameLaterVaultCoversHost(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	first := sealedEnvCredential(t, svc, "SHARED", "oma_ph_first", "other.example.com", "secret_other")
	second := sealedEnvCredential(t, svc, "SHARED", "oma_ph_second", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{first.VaultExternalID, second.VaultExternalID},
		credentials: []db.VaultCredential{first, second},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/info/refs?service=git-upload-pack", nil)
	if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != oauth2Basic("glpat-real") {
		t.Fatalf("Authorization = %q, want later vault covering this host", got)
	}
}

func TestMITMEgressPrepareGitSmartHTTPOverwritesAuthorizationAndKeepsSubstitution(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	cred := sealedEnvCredential(t, svc, "GITLAB_TOKEN", "oma_ph_git", "gitlab.example.com", "glpat-real")
	store := &fakeCredentialStore{
		vaultIDs:    []string{cred.VaultExternalID},
		credentials: []db.VaultCredential{cred},
	}
	req := newHTTPRequest(t, http.MethodGet, "https://gitlab.example.com/group/repo.git/git-upload-pack", nil)
	req.Header.Set("Authorization", "Bearer oma_ph_git")
	req.Header.Set("PRIVATE-TOKEN", "oma_ph_git")
	if err := prepareMITMEgress(t, store, svc, "gitlab.example.com:443", req); err != nil {
		t.Fatalf("Prepare: %v", err)
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
