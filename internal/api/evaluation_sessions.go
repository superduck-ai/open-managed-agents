package api

import (
	"context"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/evaluations"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type evaluationSessionControl struct {
	sessions *sessionsapi.Handler
}

func (s evaluationSessionControl) Start(ctx context.Context, input evaluations.SessionInput) (string, error) {
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

func (s evaluationSessionControl) Archive(ctx context.Context, principal auth.Principal, sessionID string) error {
	return s.sessions.ArchiveEvaluationSession(ctx, principal, sessionID)
}

func newEvaluationHandler(deps ServerDeps, sessions *sessionsapi.Handler, logger *slog.Logger) *evaluations.Handler {
	model := evaluations.NewRubricModel(deps.DB, deps.VaultSecrets)
	runner := deps.EvaluationRunner
	if runner == nil {
		runner = evaluations.NewTaskRunner(deps.DB, logger).WithRubricModel(model)
	}
	runner.WithSessions(evaluationSessionControl{sessions: sessions})
	sessions.WithSessionEventObserver(runner)
	return evaluations.NewHandler(deps.DB, logger).WithTaskRunner(runner).WithRubricModel(model)
}
