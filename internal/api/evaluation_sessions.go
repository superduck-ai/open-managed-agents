package api

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/evaluations"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type evaluationSessionStarter struct {
	sessions *sessionsapi.Handler
}

func (s evaluationSessionStarter) Start(ctx context.Context, input evaluations.SessionInput) (string, error) {
	created, err := s.sessions.CreateEvaluationSession(ctx, input.Principal, sessionsapi.EvaluationSessionSpec{
		AgentExternalID:       input.AgentExternalID,
		AgentVersion:          input.AgentVersion,
		EnvironmentExternalID: input.EnvironmentExternalID,
		Resources:             input.Resources,
		VaultIDs:              input.VaultIDs,
		Query:                 input.Query,
		Content:               input.Content,
		Title:                 input.Title,
		Metadata:              input.Metadata,
	})
	if err != nil {
		return "", err
	}
	return created.ExternalID, nil
}
