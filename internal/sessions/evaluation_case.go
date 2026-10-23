package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type EvaluationCaseSessionSpec struct {
	AgentExternalID       string
	AgentVersion          int
	EnvironmentExternalID string
	Resources             json.RawMessage
	Turns                 []string
	Metadata              map[string]string
	TimeoutSeconds        int
	Env                   json.RawMessage
}

func (h *Handler) CreateEvaluationCaseSession(ctx context.Context, principal auth.Principal, spec EvaluationCaseSessionSpec) (db.Session, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/sessions", nil)
	if err != nil {
		return db.Session{}, err
	}
	body, err := evaluationCaseSessionRequest(spec)
	if err != nil {
		return db.Session{}, invalidRequest(err)
	}
	return h.createFromBody(request, principal, body)
}

func evaluationCaseSessionRequest(spec EvaluationCaseSessionSpec) (sessionCreateRequest, error) {
	agent, err := json.Marshal(map[string]any{
		"type":    "agent",
		"id":      spec.AgentExternalID,
		"version": spec.AgentVersion,
	})
	if err != nil {
		return sessionCreateRequest{}, err
	}
	environment, err := json.Marshal(spec.EnvironmentExternalID)
	if err != nil {
		return sessionCreateRequest{}, err
	}
	metadataJSON, err := json.Marshal(evaluationCaseSessionMetadata(spec))
	if err != nil {
		return sessionCreateRequest{}, err
	}
	events := make([]map[string]any, 0, len(spec.Turns))
	for _, turn := range spec.Turns {
		events = append(events, map[string]any{
			"type":    "user.message",
			"content": []any{map[string]string{"type": "text", "text": turn}},
		})
	}
	initialEvents, err := json.Marshal(events)
	if err != nil {
		return sessionCreateRequest{}, err
	}
	request := sessionCreateRequest{sessionMutationRequest: sessionMutationRequest{
		Agent:         agent,
		EnvironmentID: environment,
		Metadata:      metadataJSON,
	}, InitialEvents: initialEvents}
	if len(spec.Resources) > 0 {
		request.Resources = spec.Resources
	}
	return request, nil
}

func evaluationCaseSessionMetadata(spec EvaluationCaseSessionSpec) map[string]string {
	metadata := make(map[string]string, len(spec.Metadata)+2)
	for key, value := range spec.Metadata {
		metadata[key] = value
	}
	if spec.TimeoutSeconds > 0 {
		metadata[sessioncontract.SandboxTimeoutMetadataKey] = strconv.Itoa(spec.TimeoutSeconds)
	}
	if len(spec.Env) > 0 {
		metadata[sessioncontract.SandboxEnvMetadataKey] = string(spec.Env)
	}
	return metadata
}
