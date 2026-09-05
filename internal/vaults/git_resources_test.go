package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

type fakeGitResourceStore struct {
	session   db.Session
	resources []db.SessionResource
	inactive  bool
}

func (s *fakeGitResourceStore) GetCodeSessionCredentialContextForIssue(_ context.Context, org, workspace, codeSession string) (db.CodeSessionCredentialContext, error) {
	if s.inactive || org != s.session.OrganizationUUID || workspace != s.session.WorkspaceUUID || codeSession != "cse_git" {
		return db.CodeSessionCredentialContext{}, db.ErrNotFound
	}
	return db.CodeSessionCredentialContext{PublicSessionUUID: s.session.UUID, PublicSessionExternalID: s.session.ExternalID}, nil
}
func (s *fakeGitResourceStore) GetSession(_ context.Context, workspace, session string) (db.Session, bool, error) {
	return s.session, workspace == s.session.WorkspaceUUID && session == s.session.ExternalID, nil
}
func (s *fakeGitResourceStore) ListSessionResources(_ context.Context, workspace, session string) ([]db.SessionResource, error) {
	if workspace != s.session.WorkspaceUUID || session != s.session.ExternalID {
		return nil, db.ErrNotFound
	}
	return s.resources, nil
}

func newGitResourceFixture(t *testing.T) (*MITMEgress, *fakeGitResourceStore, EgressSession) {
	t.Helper()
	svc := newTestSecretsService(t)
	store := &fakeGitResourceStore{
		session:   db.Session{UUID: "uuid_session", ExternalID: "ses_git", OrganizationUUID: "org_git", WorkspaceUUID: "ws_git", Status: "idle"},
		resources: []db.SessionResource{{ExternalID: "sesrsc_git", SessionExternalID: "ses_git", ResourceType: "github_repository", Payload: json.RawMessage(`{"url":"https://github.com/acme/private","mount_path":"/workspace/private"}`)}},
	}
	identity := EgressSession{OrganizationUUID: store.session.OrganizationUUID, WorkspaceUUID: store.session.WorkspaceUUID, CodeSessionExternalID: "cse_git"}
	egress := &MITMEgress{env: newEgressSubstitutor(&fakeCredentialStore{}, svc, nil), gitStore: store}
	sealGitResourceFixture(t, egress, store, "private-token-v1")
	return egress, store, identity
}
func sealGitResourceFixture(t *testing.T, egress *MITMEgress, store *fakeGitResourceStore, token string) {
	t.Helper()
	raw, err := sessionresource.SealGitHubToken(context.Background(), egress.env.secretSvc, secrets.ResourceBinding{
		OrganizationUUID: store.session.OrganizationUUID, WorkspaceUUID: store.session.WorkspaceUUID,
		OwnerKind: "session", OwnerID: store.session.ExternalID, ResourceID: store.resources[0].ExternalID,
	}, token)
	if err != nil {
		t.Fatal(err)
	}
	store.resources[0].SecretPayload = raw
}

func TestGitResourceAuthorizationScope(t *testing.T) {
	for _, tt := range []struct {
		name, method, authority, path string
		want                          bool
	}{
		{"discovery", "GET", "github.com:443", "/acme/private/info/refs?service=git-upload-pack", true},
		{"git suffix", "GET", "github.com:443", "/acme/private.git/info/refs?service=git-upload-pack", true},
		{"case insensitive repo", "POST", "github.com:443", "/ACME/Private/git-upload-pack", true},
		{"fetch", "POST", "github.com:443", "/acme/private/git-upload-pack", true},
		{"push", "POST", "github.com:443", "/acme/private/git-receive-pack", true},
		{"other repo", "POST", "github.com:443", "/acme/other/git-upload-pack", false},
		{"other owner", "POST", "github.com:443", "/other/private/git-upload-pack", false},
		{"other host", "POST", "evil.example:443", "/acme/private/git-upload-pack", false},
		{"other port", "POST", "github.com:8443", "/acme/private/git-upload-pack", false},
		{"REST", "GET", "github.com:443", "/acme/private", false},
		{"LFS", "POST", "github.com:443", "/acme/private.git/info/lfs/objects/batch", false},
		{"wrong method", "GET", "github.com:443", "/acme/private/git-upload-pack", false},
		{"wrong service", "GET", "github.com:443", "/acme/private/info/refs?service=other", false},
		{"encoded path", "POST", "github.com:443", "/acme/%70rivate/git-upload-pack", false},
		{"nested path", "POST", "github.com:443", "/acme/private/other/git-upload-pack", false},
		{"traversal", "POST", "github.com:443", "/acme/private/../other/git-upload-pack", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			egress, _, identity := newGitResourceFixture(t)
			req := newHTTPRequest(t, tt.method, tt.path, nil)
			_, err := egress.Prepare(context.Background(), identity, tt.authority, req, http.DefaultTransport)
			if err != nil {
				t.Fatal(err)
			}
			user, token, ok := req.BasicAuth()
			if tt.want {
				if !ok || user != "oauth2" || token != "private-token-v1" {
					t.Fatal("missing expected repository authorization")
				}
			} else if ok || req.Header.Get("Authorization") != "" {
				t.Fatal("credential escaped repository scope")
			}
		})
	}
}

func TestGitResourceAuthorizationReloadsRotationAndRevocation(t *testing.T) {
	egress, store, identity := newGitResourceFixture(t)
	for _, token := range []string{"private-token-v1", "private-token-v2"} {
		sealGitResourceFixture(t, egress, store, token)
		req := newHTTPRequest(t, "POST", "/acme/private/git-upload-pack", nil)
		if _, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport); err != nil {
			t.Fatal(err)
		}
		_, got, _ := req.BasicAuth()
		if got != token {
			t.Fatal("credential rotation not visible")
		}
	}
	store.resources = nil
	req := newHTTPRequest(t, "POST", "/acme/private/git-upload-pack", nil)
	if _, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("removed resource still authorizes Git")
	}
}

func TestGitResourceAuthorizationFailsClosed(t *testing.T) {
	for _, name := range []string{"inactive code session", "terminated session", "archived session", "wrong workspace", "wrong organization", "swapped resource envelope", "plaintext legacy", "duplicate repository"} {
		t.Run(name, func(t *testing.T) {
			egress, store, identity := newGitResourceFixture(t)
			switch name {
			case "inactive code session":
				store.inactive = true
			case "terminated session":
				store.session.Status = "terminated"
			case "archived session":
				now := time.Now()
				store.session.ArchivedAt = &now
			case "wrong workspace":
				identity.WorkspaceUUID = "another_workspace"
			case "wrong organization":
				identity.OrganizationUUID = "another_org"
			case "swapped resource envelope":
				store.resources[0].ExternalID = "another_resource"
			case "plaintext legacy":
				store.resources[0].SecretPayload = json.RawMessage(`{"authorization_token":"private-token-v1"}`)
			case "duplicate repository":
				store.resources = append(store.resources, store.resources[0])
			}
			req := newHTTPRequest(t, "POST", "/acme/private/git-upload-pack", nil)
			_, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport)
			if !errors.Is(err, ErrGitResourceAuthorizationRejected) {
				t.Fatalf("error = %v, want resource authorization rejected", err)
			}
			if strings.Contains(err.Error(), "private-token") || req.Header.Get("Authorization") != "" {
				t.Fatal("rejected authorization leaked credentials")
			}
		})
	}
}

func TestGitResourceAuthorizationPrecedesVaultAuthorization(t *testing.T) {
	egress, _, identity := newGitResourceFixture(t)
	svc := egress.env.secretSvc
	cred := sealedEnvCredentialWithLocation(t, svc, "GITHUB_TOKEN", "placeholder", "github.com", "host-wide-token", true, false)
	egress.env = newEgressSubstitutor(&fakeCredentialStore{vaultIDs: []string{"vlt_git"}, credentials: []db.VaultCredential{cred}}, svc, nil)
	req := newHTTPRequest(t, "POST", "/acme/private/git-upload-pack", nil)
	if _, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport); err != nil {
		t.Fatal(err)
	}
	_, token, _ := req.BasicAuth()
	if token != "private-token-v1" {
		t.Fatal("host-wide credential overrode resource credential")
	}
}

func TestAnonymousGitResourceSuppressesCredentials(t *testing.T) {
	egress, store, identity := newGitResourceFixture(t)
	sealGitResourceFixture(t, egress, store, "")
	req := newHTTPRequest(t, "GET", "/acme/private/info/refs?service=git-upload-pack", nil)
	req.Header.Set("Authorization", "Bearer sandbox-supplied")
	if _, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("anonymous resource received credentials")
	}
	store.inactive = true
	if _, err := egress.Prepare(context.Background(), identity, "github.com:443", req, http.DefaultTransport); err == nil {
		t.Fatal("anonymous resource bypassed session authorization")
	}
}
