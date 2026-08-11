package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

// ManagedAgentCreateInput 汇总为 managed agent 创建 code session 和签发 sandbox 凭证所需的上下文。
type ManagedAgentCreateInput struct {
	Session                    db.Session
	Environment                db.Environment
	EnvironmentWork            db.EnvironmentWork
	Model                      string
	Title                      string
	WorkDir                    string
	PermissionMode             string
	DangerouslySkipPermissions bool
	Config                     json.RawMessage
}

// ManagedAgentCreateResult 只在创建链路内短暂携带两份明文凭证，调用方应立即交给
// environment-manager 的文件描述符合同，不能写入数据库或 session metadata。
type ManagedAgentCreateResult struct {
	CodeSessionID       string
	PublicSessionID     string
	SDKURLPath          string
	OAuthAccessToken    string
	SessionIngressToken string
}

// CreateManagedAgentCodeSession 原子地建立 code-session 身份上下文，并为 sandbox
// 分别签发 Messages OAuth-compatible token 与 worker session-ingress JWT。
func (s *Service) CreateManagedAgentCodeSession(ctx context.Context, input ManagedAgentCreateInput) (ManagedAgentCreateResult, error) {
	if strings.TrimSpace(input.EnvironmentWork.UUID) == "" {
		return ManagedAgentCreateResult{}, errors.New("managed agent environment work is required")
	}
	codeSessionID, err := ids.New("cse_")
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	oauthAccessToken, err := newOAuthCompatibleToken()
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	now := time.Now().UTC()
	metadata, err := managedAgentCodeSessionMetadata(input)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	record, err := s.db.CreateCodeSession(ctx, db.CreateCodeSessionInput{
		ExternalID:            codeSessionID,
		OrganizationUUID:      input.Session.OrganizationUUID,
		WorkspaceUUID:         input.Session.WorkspaceUUID,
		SessionUUID:           input.Session.UUID,
		SessionExternalID:     input.Session.ExternalID,
		EnvironmentUUID:       input.Environment.UUID,
		EnvironmentExternalID: input.Environment.ExternalID,
		WorkDir:               strings.TrimSpace(input.WorkDir),
		PermissionMode:        strings.TrimSpace(input.PermissionMode),
		Model:                 strings.TrimSpace(input.Model),
		Status:                "initializing",
		Metadata:              metadata,
		// OAuth-compatible token 只落 SHA-256 hash；明文仅存在于当前返回值中。
		OAuthAccessTokenHash: auth.HashAPIKey(oauthAccessToken),
		CreatedAt:            now,
	})
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	needsCleanup := true
	defer func() {
		if !needsCleanup {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if cleanupErr := s.db.TerminateManagedAgentCodeSession(
			cleanupCtx,
			input.Session.OrganizationUUID,
			input.Session.WorkspaceUUID,
			record.ExternalID,
		); cleanupErr != nil {
			s.logger.ErrorContext(
				cleanupCtx,
				"terminate incomplete managed agent code session",
				"code_session_id", record.ExternalID,
				"error", cleanupErr,
			)
		}
	}()
	if err := s.queueInitialize(ctx, record, input.Config, now); err != nil {
		return ManagedAgentCreateResult{}, err
	}
	if err := s.ActivateManagedAgentCodeSession(ctx, record); err != nil {
		return ManagedAgentCreateResult{}, err
	}
	credentialContext, err := s.db.GetCodeSessionCredentialContextForIssue(
		ctx,
		input.Session.OrganizationUUID,
		input.Session.WorkspaceUUID,
		record.ExternalID,
	)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	// 重新从数据库读取签发上下文，保证 JWT claims 与实际持久化的租户和 agent 一致。
	sessionIngressToken, err := s.issueSessionIngressToken(credentialContext)
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	needsCleanup = false
	return ManagedAgentCreateResult{
		CodeSessionID:       record.ExternalID,
		PublicSessionID:     record.SessionExternalID,
		SDKURLPath:          "/v1/code/sessions/" + record.ExternalID,
		OAuthAccessToken:    oauthAccessToken,
		SessionIngressToken: sessionIngressToken,
	}, nil
}

// ActivateManagedAgentCodeSession locks the owning Session, replays complete
// public history in stable order, and activates the Code Session atomically.
func (s *Service) ActivateManagedAgentCodeSession(
	ctx context.Context,
	codeSession db.CodeSession,
) error {
	if s == nil || s.db == nil {
		return db.ErrNotFound
	}
	return s.db.WithManagedAgentActivationTx(ctx, func(tx db.ManagedAgentActivationTx) error {
		// lock session by session external id
		lockedSession, err := tx.LockSessionForEvents(
			ctx,
			codeSession.WorkspaceUUID,
			codeSession.SessionExternalID,
		)
		if err != nil {
			return err
		}
		// lock code_session by code session id
		lockedCodeSession, err := tx.LockInitializingCodeSession(
			ctx,
			codeSession.WorkspaceUUID,
			codeSession.UUID,
		)
		if err != nil {
			return err
		}
		sessionEvents, err := tx.ListSessionEventsForActivation(ctx, lockedSession)
		if err != nil {
			return err
		}
		storedBindings, err := tx.ListSessionEventFileBindings(ctx, lockedSession)
		if err != nil {
			return err
		}
		inboundInputs := make([]db.AppendCodeSessionEventInput, 0, len(sessionEvents))
		for _, event := range sessionEvents {
			if !shouldForwardPublicEventToWorker(event.EventType) {
				continue
			}
			inbound, err := s.convertSessionEventToInbound(lockedCodeSession.ExternalID, event, storedBindings)
			if err != nil {
				return err
			}
			inboundInputs = append(inboundInputs, inbound)
		}
		if err := tx.AppendCodeSessionInboundEvents(ctx, lockedCodeSession, inboundInputs); err != nil {
			return err
		}
		activated, err := tx.ActivateCodeSession(ctx, lockedCodeSession.UUID, time.Now().UTC())
		if err != nil {
			return err
		}
		if !activated {
			return db.ErrInvalidState
		}
		return nil
	})
}

// convertSessionEventToInbound maps one public session event payload into a
// Code Session inbound append input.
func (s *Service) convertSessionEventToInbound(
	codeSessionID string,
	event db.SessionEvent,
	fileBindings []sessioncontract.EventFileBinding,
) (db.AppendCodeSessionEventInput, error) {
	payload, err := workerPayloadForPublicEvent(codeSessionID, event.Payload, event.ProcessedAt, fileBindings)
	if err != nil {
		return db.AppendCodeSessionEventInput{}, err
	}
	return newInboundEventInput(codeSessionID, payload, "public-session")
}

// TerminateManagedAgentCodeSession revokes a Code Session created for a
// sandbox launch that failed before the runtime became usable.
func (s *Service) TerminateManagedAgentCodeSession(
	ctx context.Context,
	session db.Session,
	codeSessionID string,
) error {
	if s == nil {
		return nil
	}
	return s.db.TerminateManagedAgentCodeSession(
		ctx,
		session.OrganizationUUID,
		session.WorkspaceUUID,
		strings.TrimSpace(codeSessionID),
	)
}

func managedAgentCodeSessionMetadata(input ManagedAgentCreateInput) (json.RawMessage, error) {
	// metadata 只记录非秘密运行信息，两份明文凭证都不进入 JSON。
	return marshalRaw(map[string]any{
		"source":                         "managed_agents_local",
		"public_session_id":              input.Session.ExternalID,
		"environment_id":                 input.Environment.ExternalID,
		"title":                          input.Title,
		"config":                         rawObject(input.Config),
		"dangerously_skip_permissions":   input.DangerouslySkipPermissions,
		"managed_agent_session_work_dir": strings.TrimSpace(input.WorkDir),
	})
}
