package vaults

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

// gitResourceStore reuses scoped lookups; the session and token are read again
// on every Git request so rotation and session termination take effect immediately.
type gitResourceStore interface {
	GetCodeSessionCredentialContextForIssue(context.Context, string, string, string) (db.CodeSessionCredentialContext, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
	ListSessionResources(context.Context, string, string) ([]db.SessionResource, error)
}

func (e *MITMEgress) authorizeGitResource(ctx context.Context, identity EgressSession, host, port string, req *http.Request) (bool, error) {
	repoPath := gitSmartHTTPRepositoryPath(req)
	if e.gitStore == nil || host != "github.com" || port != "443" || repoPath == "" {
		return false, nil
	}
	resources, err := e.loadGitResources(ctx, identity)
	if err != nil {
		return false, gitResourceAuthorizationRejected(err)
	}
	var match *db.SessionResource
	for i := range resources {
		resource := &resources[i]
		if resource.ResourceType != "github_repository" || resource.DeletedAt != nil {
			continue
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(resource.Payload, &payload); err != nil {
			return false, gitResourceAuthorizationRejected(err)
		}
		if !strings.EqualFold(payload.URL, "https://github.com"+repoPath) {
			continue
		}
		if match != nil {
			return false, gitResourceAuthorizationRejected(errAmbiguousGitResource)
		}
		match = resource
	}
	if match == nil {
		return false, nil
	}
	token, err := sessionresource.OpenGitHubToken(ctx, e.env.secretSvc, secrets.ResourceBinding{
		OrganizationUUID: identity.OrganizationUUID,
		WorkspaceUUID:    identity.WorkspaceUUID,
		OwnerKind:        "session",
		OwnerID:          match.SessionExternalID,
		ResourceID:       match.ExternalID,
	}, match.SecretPayload)
	if err != nil {
		return false, gitResourceAuthorizationRejected(err)
	}
	req.Header.Del("Authorization")
	if token != "" {
		setGitSmartHTTPAuthorization(req.Header, token)
	}
	return true, nil
}

func (e *MITMEgress) loadGitResources(ctx context.Context, identity EgressSession) ([]db.SessionResource, error) {
	parent, err := e.gitStore.GetCodeSessionCredentialContextForIssue(ctx,
		identity.OrganizationUUID, identity.WorkspaceUUID, identity.CodeSessionExternalID)
	if err != nil {
		return nil, err
	}
	session, found, err := e.gitStore.GetSession(ctx, identity.WorkspaceUUID, parent.PublicSessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found || session.OrganizationUUID != identity.OrganizationUUID || session.UUID != parent.PublicSessionUUID ||
		session.ArchivedAt != nil || session.DeletedAt != nil || session.Status == "terminated" {
		return nil, db.ErrNotFound
	}
	return e.gitStore.ListSessionResources(ctx, identity.WorkspaceUUID, session.ExternalID)
}

// GitHub resource URLs are unescaped owner/repo paths. Reject ambiguous request
// paths rather than broadening a credential to another endpoint on the same host.
func gitSmartHTTPRepositoryPath(req *http.Request) string {
	if !isGitSmartHTTPRequest(req) || strings.Contains(req.URL.EscapedPath(), "%") {
		return ""
	}
	path := req.URL.Path
	for _, suffix := range []string{"/info/refs", "/git-upload-pack", "/git-receive-pack"} {
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		path = strings.TrimSuffix(strings.TrimSuffix(path, suffix), ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 3 || parts[0] != "" || parts[1] == "" || parts[2] == "" ||
			parts[1] == "." || parts[1] == ".." || parts[2] == "." || parts[2] == ".." {
			return ""
		}
		return path
	}
	return ""
}
