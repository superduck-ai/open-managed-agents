package api

import (
	"context"
	"errors"
	"log/slog"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	evaluationsapi "github.com/superduck-ai/open-managed-agents/internal/evaluations"
	filestoreapi "github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

const agentScoreFileLimit = 1 << 20

type evaluationScoreSessionCreator interface {
	CreateEvaluationCaseSession(context.Context, auth.Principal, sessionsapi.EvaluationCaseSessionSpec) (db.Session, error)
}

type evaluationScoreSessionLookup interface {
	GetSession(context.Context, string, string) (db.Session, bool, error)
}

type evaluationScoreFiles interface {
	WriteResult(context.Context, string, string, []byte) error
	ReadScore(context.Context, string, string) ([]byte, error)
}

type evaluationAgentScoreSandbox struct {
	create evaluationScoreSessionCreator
	lookup evaluationScoreSessionLookup
	files  evaluationScoreFiles
	logger *slog.Logger
}

func newEvaluationAgentScoreSandbox(sessions *sessionsapi.Handler, database *db.DB, files *filestoreapi.Service, logger *slog.Logger) evaluationsapi.AgentScoreSandbox {
	sandbox := &evaluationAgentScoreSandbox{logger: logging.LoggerOrDefault(logger)}
	if sessions != nil {
		sandbox.create = sessions
	}
	if database != nil {
		sandbox.lookup = database
	}
	if files != nil {
		sandbox.files = filestoreEvaluationScoreFiles{files: files}
	}
	return sandbox
}

func (s *evaluationAgentScoreSandbox) Start(ctx context.Context, input evaluationsapi.AgentScoreInput) (string, error) {
	if s.create == nil {
		return "", errors.New("evaluation agent score sandbox is not configured")
	}
	created, err := s.create.CreateEvaluationCaseSession(ctx, auth.Principal{
		OrganizationUUID: input.OrganizationUUID,
		WorkspaceUUID:    input.WorkspaceUUID,
		UserUUID:         input.UserUUID,
	}, sessionsapi.EvaluationCaseSessionSpec{
		AgentExternalID:       input.AgentExternalID,
		AgentVersion:          input.AgentVersion,
		EnvironmentExternalID: input.EnvironmentExternalID,
		Turns:                 input.Turns,
		Metadata:              input.Metadata,
	})
	if err != nil {
		return "", err
	}
	if s.files != nil && len(input.ResultJSON) > 0 {
		if writeErr := s.files.WriteResult(ctx, input.WorkspaceUUID, created.ExternalID, input.ResultJSON); writeErr != nil {
			logging.LoggerOrDefault(s.logger).WarnContext(ctx, "seed evaluation score result",
				"organization_id", input.OrganizationUUID,
				"workspace_id", input.WorkspaceUUID,
				"session_id", created.ExternalID,
				"error", writeErr,
			)
		}
	}
	return created.ExternalID, nil
}

func (s *evaluationAgentScoreSandbox) Claim(ctx context.Context, organizationUUID, workspaceUUID, sessionID string) (evaluationsapi.AgentScoreClaim, bool, error) {
	if s.lookup == nil {
		return evaluationsapi.AgentScoreClaim{}, false, nil
	}
	session, ok, err := s.lookup.GetSession(ctx, workspaceUUID, sessionID)
	if err != nil || !ok {
		return evaluationsapi.AgentScoreClaim{}, false, err
	}
	if session.OrganizationUUID != organizationUUID || session.WorkspaceUUID != workspaceUUID {
		return evaluationsapi.AgentScoreClaim{}, false, nil
	}
	claim, ok := evaluationsapi.AgentScoreClaimFromMetadata(session.Metadata)
	if !ok {
		return evaluationsapi.AgentScoreClaim{}, false, nil
	}
	claim.OrganizationUUID = session.OrganizationUUID
	claim.WorkspaceUUID = session.WorkspaceUUID
	return claim, true, nil
}

func (s *evaluationAgentScoreSandbox) ReadScore(ctx context.Context, _, workspaceUUID, sessionID string) ([]byte, error) {
	if s.files == nil {
		return nil, evaluationsapi.ErrAgentScoreFileMissing
	}
	body, err := s.files.ReadScore(ctx, workspaceUUID, sessionID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, evaluationsapi.ErrAgentScoreFileMissing
	}
	return body, err
}

type filestoreEvaluationScoreFiles struct {
	files *filestoreapi.Service
}

func (f filestoreEvaluationScoreFiles) WriteResult(ctx context.Context, workspaceUUID, sessionExternalID string, body []byte) error {
	return f.files.WriteSessionFile(ctx, workspaceUUID, sessionExternalID, evaluationsapi.AgentScoreResultDirectory, evaluationsapi.AgentScoreResultPath, "application/json", body)
}

func (f filestoreEvaluationScoreFiles) ReadScore(ctx context.Context, workspaceUUID, sessionExternalID string) ([]byte, error) {
	return f.files.ReadSessionFile(ctx, workspaceUUID, sessionExternalID, evaluationsapi.AgentScoreOutputPath, agentScoreFileLimit)
}
