package sessions

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
)

func (h *Handler) isOfficialSDKFixturePrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" && principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) createUsesOfficialFixtures(fields map[string]json.RawMessage) bool {
	agentRaw := fields["agent"]
	env, _ := parseRequiredStringField(fields, "environment_id")
	if env != h.cfg.SDKFixtures.EnvironmentID {
		return false
	}
	var agentID string
	if json.Unmarshal(agentRaw, &agentID) == nil {
		return agentID == h.cfg.SDKFixtures.AgentID
	}
	var object struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(agentRaw, &object)
	return object.ID == h.cfg.SDKFixtures.AgentID
}

func (h *Handler) isFixtureResource(r *http.Request, sessionID, resourceID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID && resourceID == h.cfg.SDKFixtures.SessionResourceID
}

func (h *Handler) isFixtureThread(r *http.Request, sessionID, threadID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID && threadID == h.cfg.SDKFixtures.SessionThreadID
}

func (h *Handler) isOfficialSDKFixtureSession(r *http.Request, sessionID string) bool {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID
}

func normalizeFixtureEvent(raw json.RawMessage, now time.Time) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, errors.New("event must be an object")
	}
	eventType, _ := payload["type"].(string)
	if !allowedPublicEventType(eventType) {
		return nil, errors.New("event type is not accepted by this endpoint")
	}
	if err := validatePublicInputEvent(eventType, payload); err != nil {
		return nil, err
	}
	eventID, err := ids.New("sevt_")
	if err != nil {
		return nil, err
	}
	payload["id"] = eventID
	payload["processed_at"] = now.Format(time.RFC3339)
	return httpapi.MarshalRaw(payload)
}

func (h *Handler) fixtureDBSession(principal auth.Principal) db.Session {
	now := time.Now().UTC()
	return db.Session{
		ID:                    1,
		UUID:                  uuid.NewString(),
		ExternalID:            h.cfg.SDKFixtures.SessionID,
		OrganizationID:        principal.OrganizationID,
		WorkspaceID:           principal.WorkspaceID,
		CreatedByAPIKeyID:     principal.APIKeyID,
		EnvironmentExternalID: h.cfg.SDKFixtures.EnvironmentID,
		AgentExternalID:       h.cfg.SDKFixtures.AgentID,
		AgentVersion:          1,
		AgentSnapshot:         h.fixtureAgentSnapshot(),
		Metadata:              json.RawMessage(`{"foo":"string"}`),
		VaultIDs:              json.RawMessage(`["string"]`),
		Status:                "idle",
		Usage:                 json.RawMessage(`{}`),
		Stats:                 json.RawMessage(`{}`),
		OutcomeEvaluations:    json.RawMessage(`[]`),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func (h *Handler) fixtureSession(now time.Time, archived bool) sessionResponse {
	var archivedAt *string
	if archived {
		value := httpapi.FormatTime(now)
		archivedAt = &value
	}
	title := "Order #1234 inquiry"
	return sessionResponse{
		ID:                 h.cfg.SDKFixtures.SessionID,
		Agent:              h.fixtureAgentSnapshot(),
		ArchivedAt:         archivedAt,
		CreatedAt:          httpapi.FormatTime(now),
		EnvironmentID:      h.cfg.SDKFixtures.EnvironmentID,
		Metadata:           json.RawMessage(`{"foo":"string"}`),
		OutcomeEvaluations: json.RawMessage(`[]`),
		Resources:          []json.RawMessage{h.fixtureResource(now)},
		Stats:              json.RawMessage(`{}`),
		Status:             "idle",
		Title:              &title,
		Type:               "session",
		UpdatedAt:          httpapi.FormatTime(now),
		Usage:              json.RawMessage(`{}`),
		VaultIDs:           json.RawMessage(`["string"]`),
	}
}

func (h *Handler) fixtureAgentSnapshot() json.RawMessage {
	raw, _ := httpapi.MarshalRaw(map[string]any{
		"id":          h.cfg.SDKFixtures.AgentID,
		"description": nil,
		"mcp_servers": []any{},
		"model":       map[string]any{"id": "claude-opus-4-6", "speed": "standard"},
		"multiagent":  nil,
		"name":        "fixture agent",
		"skills":      []any{},
		"system":      nil,
		"tools":       []any{},
		"type":        "agent",
		"version":     1,
	})
	return raw
}

func (h *Handler) fixtureResource(now time.Time) json.RawMessage {
	raw, _ := httpapi.MarshalRaw(map[string]any{
		"id":         h.cfg.SDKFixtures.SessionResourceID,
		"created_at": httpapi.FormatTime(now),
		"file_id":    "file_011CNha8iCJcU1wXNR6q4V8w",
		"mount_path": "/uploads/receipt.pdf",
		"type":       "file",
		"updated_at": httpapi.FormatTime(now),
	})
	return raw
}

func (h *Handler) fixtureThread(now time.Time, archived bool) threadResponse {
	var archivedAt *string
	if archived {
		value := httpapi.FormatTime(now)
		archivedAt = &value
	}
	return threadResponse{
		ID:             h.cfg.SDKFixtures.SessionThreadID,
		Agent:          h.fixtureAgentSnapshot(),
		ArchivedAt:     archivedAt,
		CreatedAt:      httpapi.FormatTime(now),
		ParentThreadID: nil,
		SessionID:      h.cfg.SDKFixtures.SessionID,
		Stats:          json.RawMessage(`{}`),
		Status:         "idle",
		Type:           "session_thread",
		UpdatedAt:      httpapi.FormatTime(now),
		Usage:          json.RawMessage(`{}`),
	}
}
