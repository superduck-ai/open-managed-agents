package vaults

import (
	"context"
	"encoding/json"
	"net"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

// gitResourceStore reloads session state and credentials on each request so rotation and termination take effect.
type gitResourceStore interface {
	GetCodeSessionCredentialContextForIssue(context.Context, string, string, string) (db.CodeSessionCredentialContext, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
	ListSessionResources(context.Context, string, string) ([]db.SessionResource, error)
}

// authorizeGitResource returns true for matched anonymous repositories too, preventing Vault fallback.
func (e *MITMEgress) authorizeGitResource(ctx context.Context, identity EgressSession, host, port string, req *http.Request) (bool, error) {
	repoPath := gitSmartHTTPRepositoryPath(req)
	if e.gitStore == nil || repoPath == "" {
		return false, nil
	}
	requestKey, err := sessionresource.GitRepositoryKey("https://" + net.JoinHostPort(host, port) + repoPath)
	if err != nil {
		return false, gitResourceAuthorizationRejected(err)
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
		resourceKey, err := sessionresource.GitRepositoryKey(payload.URL)
		if err != nil {
			return false, gitResourceAuthorizationRejected(err)
		}
		if resourceKey != requestKey {
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
	token, err := sessionresource.DecryptGitToken(ctx, e.env.secretSvc, secrets.ResourceBinding{
		OrganizationUUID: identity.OrganizationUUID,
		WorkspaceUUID:    identity.WorkspaceUUID,
	}, match.SecretPayload)
	if err != nil {
		return false, gitResourceAuthorizationRejected(err)
	}
	// Anonymous resources must also discard client-supplied credentials.
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
