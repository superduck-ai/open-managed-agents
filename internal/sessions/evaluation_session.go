package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type EvaluationSessionSpec struct {
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	Resources             json.RawMessage
	VaultIDs              []string
	Query                 string
	Content               json.RawMessage
	Title                 string
	Metadata              map[string]string
}

func (h *Handler) CreateEvaluationSession(ctx context.Context, principal auth.Principal, spec EvaluationSessionSpec) (db.Session, error) {
	request, err := http.NewRequestWithContext(auth.WithPrincipal(ctx, principal), http.MethodPost, "/v1/sessions", nil)
	if err != nil {
		return db.Session{}, err
	}
	body, err := evaluationSessionRequest(spec)
	if err != nil {
		return db.Session{}, invalidRequest(err)
	}
	return h.createFromBody(request, principal, body)
}

func (h *Handler) ArchiveEvaluationSession(ctx context.Context, principal auth.Principal, sessionID string) error {
	_, err := h.archiveSession(ctx, principal, sessionID)
	return err
}

func evaluationSessionRequest(spec EvaluationSessionSpec) (sessionCreateRequest, error) {
	agent := map[string]any{
		"type":    "agent",
		"id":      spec.AgentExternalID,
		"version": spec.AgentVersion,
	}
	agentJSON, err := json.Marshal(agent)
	if err != nil {
		return sessionCreateRequest{}, err
	}
	environment, err := json.Marshal(spec.EnvironmentExternalID)
	if err != nil {
		return sessionCreateRequest{}, err
	}
	metadata := spec.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return sessionCreateRequest{}, err
	}
	events, err := json.Marshal([]map[string]any{{
		"type":    "user.message",
		"content": evaluationMessageContent(spec),
	}})
	if err != nil {
		return sessionCreateRequest{}, err
	}
	request := sessionCreateRequest{sessionMutationRequest: sessionMutationRequest{
		Agent:         agentJSON,
		EnvironmentID: environment,
		Metadata:      metadataJSON,
	}, InitialEvents: events}
	if title := strings.TrimSpace(spec.Title); title != "" {
		encoded, err := json.Marshal(title)
		if err != nil {
			return sessionCreateRequest{}, err
		}
		request.Title = encoded
	}
	if len(spec.VaultIDs) > 0 {
		encoded, err := json.Marshal(spec.VaultIDs)
		if err != nil {
			return sessionCreateRequest{}, err
		}
		request.VaultIDs = encoded
	}
	if len(spec.Resources) > 0 && string(spec.Resources) != "null" {
		request.Resources = spec.Resources
	}
	return request, nil
}

func evaluationMessageContent(spec EvaluationSessionSpec) any {
	var blocks []json.RawMessage
	if json.Unmarshal(spec.Content, &blocks) == nil && len(blocks) > 0 {
		return blocks
	}
	return []any{map[string]string{"type": "text", "text": spec.Query}}
}
