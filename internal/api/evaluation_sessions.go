package api

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	evaluationsapi "github.com/superduck-ai/open-managed-agents/internal/evaluations"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type evaluationCaseSessionStarter struct {
	sessions *sessionsapi.Handler
}

func (s evaluationCaseSessionStarter) StartCaseSession(ctx context.Context, principal auth.Principal, input evaluationsapi.CaseSessionInput) (string, error) {
	created, err := s.sessions.CreateEvaluationCaseSession(ctx, principal, sessionsapi.EvaluationCaseSessionSpec{
		AgentExternalID:       input.AgentExternalID,
		AgentVersion:          input.AgentVersion,
		EnvironmentExternalID: input.EnvironmentExternalID,
		Resources:             input.Resources,
		Turns:                 input.Turns,
		Metadata:              input.Metadata,
	})
	if err != nil {
		return "", err
	}
	return created.ExternalID, nil
}
