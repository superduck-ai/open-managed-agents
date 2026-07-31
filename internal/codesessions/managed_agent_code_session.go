package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
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
	if input.EnvironmentWork.ID <= 0 {
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
		OrganizationID:        input.Session.OrganizationID,
		WorkspaceID:           input.Session.WorkspaceID,
		SessionID:             input.Session.ID,
		SessionExternalID:     input.Session.ExternalID,
		EnvironmentID:         input.Environment.ID,
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
			input.Session.OrganizationID,
			input.Session.WorkspaceID,
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
	if err := s.activateManagedAgentCodeSession(ctx, input.Session, record); err != nil {
		return ManagedAgentCreateResult{}, err
	}
	credentialContext, err := s.db.GetCodeSessionCredentialContextForIssue(
		ctx,
		input.Session.OrganizationID,
		input.Session.WorkspaceID,
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

// activateManagedAgentCodeSession merges public session history with the
// startup queue into inbound, then activates under the same Session lock as send.
//
// Order: historical forwardable session_events (excluding UUIDs still in the
// queue) first, then queue items in FIFO order. Queue remains the sole source of
// startup-window responsibility and cutover matching.
func (s *Service) activateManagedAgentCodeSession(
	ctx context.Context,
	session db.Session,
	codeSession db.CodeSession,
) error {
	for {
		items, err := s.db.ListSessionEventQueueItems(ctx, session)
		if err != nil {
			return err
		}
		history, err := s.listSessionEventsAscending(ctx, session)
		if err != nil {
			return err
		}
		queuedUUIDs := lo.SliceToMap(items, func(item db.SessionEventQueueItem) (string, struct{}) {
			return item.Event.UUID, struct{}{}
		})
		historyOnly := lo.Filter(history, func(event db.SessionEvent, _ int) bool {
			_, queued := queuedUUIDs[event.UUID]
			return !queued
		})
		historyInputs, err := s.inboundInputsFromPublicSessionEvents(codeSession.ExternalID, historyOnly)
		if err != nil {
			return err
		}
		queueInputs, err := lo.MapErr(items, func(item db.SessionEventQueueItem, _ int) (db.AppendCodeSessionEventInput, error) {
			if item.Event.EventType != "user.message" {
				return db.AppendCodeSessionEventInput{}, fmt.Errorf(
					"%w: session event queue contains a non-user message",
					db.ErrInvalidState,
				)
			}
			return s.inboundInputFromPublicSessionEvent(codeSession.ExternalID, item.Event)
		})
		if err != nil {
			return err
		}
		inputs := append(historyInputs, queueInputs...)
		activated, err := s.db.ActivateManagedAgentCodeSessionWithQueue(
			ctx,
			codeSession,
			items,
			inputs,
		)
		if err != nil {
			return err
		}
		if activated {
			return nil
		}
	}
}

func (s *Service) listSessionEventsAscending(ctx context.Context, session db.Session) ([]db.SessionEvent, error) {
	var out []db.SessionEvent
	var cursor *db.SessionEventPageCursor
	for {
		events, hasMore, err := s.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceID:       session.WorkspaceID,
			SessionExternalID: session.ExternalID,
			Limit:             100,
			Cursor:            cursor,
			Order:             "asc",
		})
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
		if !hasMore || len(events) == 0 {
			return out, nil
		}
		last := events[len(events)-1]
		cursor = &db.SessionEventPageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

func (s *Service) inboundInputsFromPublicSessionEvents(
	codeSessionID string,
	events []db.SessionEvent,
) ([]db.AppendCodeSessionEventInput, error) {
	inputs := make([]db.AppendCodeSessionEventInput, 0, len(events))
	for _, event := range events {
		if !forwardPublicEventToWorker(event.EventType) {
			continue
		}
		input, err := s.inboundInputFromPublicSessionEvent(codeSessionID, event)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func (s *Service) inboundInputFromPublicSessionEvent(
	codeSessionID string,
	event db.SessionEvent,
) (db.AppendCodeSessionEventInput, error) {
	payload, err := workerPayloadForPublicEvent(codeSessionID, event.Payload, event.ProcessedAt)
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
		session.OrganizationID,
		session.WorkspaceID,
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
