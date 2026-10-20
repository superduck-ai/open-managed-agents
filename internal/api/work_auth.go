package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (s *Server) authenticateWorkToken(r *http.Request, token string) (auth.Principal, *httpapi.Error) {
	denied := func() (auth.Principal, *httpapi.Error) {
		return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid work credential")
	}
	scope, ok := auth.ParseWorkToken(token)
	if !ok {
		return denied()
	}
	work, err := s.db.GetEnvironmentWork(r.Context(), scope.WorkspaceUUID, scope.EnvironmentID, scope.WorkID)
	if err != nil || !auth.MatchesWorkSecret(token, work.Secret) || !workCredentialActive(work, time.Now()) {
		return denied()
	}
	session, found, err := s.db.GetSession(r.Context(), work.WorkspaceUUID, work.SessionExternalID)
	if err != nil || !found || session.ArchivedAt != nil || session.DeletedAt != nil || session.EnvironmentUUID != work.EnvironmentUUID || session.OrganizationUUID != work.OrganizationUUID {
		return denied()
	}
	resources, err := s.db.ListSessionResources(r.Context(), work.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return denied()
	}
	if !workCredentialAllows(r.Method, r.URL.Path, work, resources) {
		return auth.Principal{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Work credential does not allow this operation")
	}
	return auth.Principal{
		CredentialType: auth.CredentialTypeWorkToken, OrganizationUUID: work.OrganizationUUID,
		WorkspaceUUID: work.WorkspaceUUID, EnvironmentUUID: work.EnvironmentUUID,
		EnvironmentExternalID: work.EnvironmentExternalID, PublicSessionUUID: session.UUID,
		PublicSessionExternalID: session.ExternalID,
	}, nil
}

func workCredentialActive(work db.EnvironmentWork, now time.Time) bool {
	if work.DeletedAt != nil || work.StoppedAt != nil || work.StopRequestedAt != nil || (work.State != "starting" && work.State != "active") {
		return false
	}
	if work.LatestHeartbeatAt != nil && work.HeartbeatTTLSeconds != nil {
		return *work.HeartbeatTTLSeconds > 0 && now.Before(work.LatestHeartbeatAt.Add(time.Duration(*work.HeartbeatTTLSeconds)*time.Second))
	}
	if work.AcknowledgedAt != nil {
		return now.Before(work.AcknowledgedAt.Add(60 * time.Second))
	}
	return work.ClaimExpiresAt != nil && now.Before(*work.ClaimExpiresAt)
}

func workCredentialAllows(method, path string, work db.EnvironmentWork, resources []db.SessionResource) bool {
	sessionPath := "/v1/sessions/" + work.SessionExternalID
	if method == http.MethodGet && (path == sessionPath || path == sessionPath+"/events" || path == sessionPath+"/events/stream") {
		return true
	}
	if method == http.MethodPost && path == sessionPath+"/events" {
		return true
	}
	workPath := "/v1/environments/" + work.EnvironmentExternalID + "/work/" + work.ExternalID
	if method == http.MethodPost && (path == workPath+"/heartbeat" || path == workPath+"/stop") {
		return true
	}
	for _, resource := range resources {
		if resource.DeletedAt != nil || resource.ResourceType != db.SessionResourceTypeMemoryStore {
			continue
		}
		var payload struct {
			MemoryStoreID string `json:"memory_store_id"`
			Access        string `json:"access"`
		}
		if json.Unmarshal(resource.Payload, &payload) != nil || payload.MemoryStoreID == "" {
			continue
		}
		prefix := "/v1/memory_stores/" + payload.MemoryStoreID
		if method == http.MethodGet && (path == prefix || path == prefix+"/memories" || strings.HasPrefix(path, prefix+"/memories/")) {
			return true
		}
		if payload.Access == "read_write" && (path == prefix+"/memories" || strings.HasPrefix(path, prefix+"/memories/")) && (method == http.MethodPost || method == http.MethodPatch || method == http.MethodDelete) {
			return true
		}
	}
	return false
}
