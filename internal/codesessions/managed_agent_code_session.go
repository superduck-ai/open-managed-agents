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
	if err := s.activateManagedAgentCodeSession(ctx, input.Session, record); err != nil {
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

// activateManagedAgentCodeSession loads the startup queue and public session
// history outside the activation transaction, converts them to inbound inputs,
// then commits activation under the same Session row lock used by Send Events.
//
// Inbound order after initialize: forwardable session history excluding events
// still referenced by the startup queue, then queue items in FIFO order. The
// queue snapshot is the only cutover signal: if it changes before commit, the
// loop reloads and retries.
func (s *Service) activateManagedAgentCodeSession(
	ctx context.Context,
	session db.Session,
	codeSession db.CodeSession,
) error {
	for {
		queueItems, err := s.db.ListSessionEventQueueItems(ctx, session)
		if err != nil {
			return err
		}
		sessionEvents, err := s.listAllSessionEvents(ctx, session)
		if err != nil {
			return err
		}
		queuedEventUUIDs := lo.SliceToMap(queueItems, func(item db.SessionEventQueueItem) (string, struct{}) {
			return item.Event.UUID, struct{}{}
		})
		sessionEventsOutsideQueue := lo.Filter(sessionEvents, func(event db.SessionEvent, _ int) bool {
			_, queued := queuedEventUUIDs[event.UUID]
			return !queued
		})
		historyInbound, err := s.convertSessionEventsToInbound(codeSession.ExternalID, sessionEventsOutsideQueue)
		if err != nil {
			return err
		}
		queueInbound, err := lo.MapErr(queueItems, func(item db.SessionEventQueueItem, _ int) (db.AppendCodeSessionEventInput, error) {
			if item.Event.EventType != "user.message" {
				return db.AppendCodeSessionEventInput{}, fmt.Errorf(
					"%w: session event queue contains a non-user message",
					db.ErrInvalidState,
				)
			}
			return s.convertSessionEventToInbound(codeSession.ExternalID, item.Event)
		})
		if err != nil {
			return err
		}
		inboundInputs := append(historyInbound, queueInbound...)
		committed, err := s.CommitManagedAgentCodeSessionActivation(
			ctx,
			codeSession,
			queueItems,
			inboundInputs,
		)
		if err != nil {
			return err
		}
		if committed {
			return nil
		}
	}
}

// CommitManagedAgentCodeSessionActivation writes inbound inputs, clears the
// matched startup queue snapshot, and marks the Code Session active in one
// transaction. It returns committed=false when the locked queue no longer
// matches queueItems so the caller can reload and retry without partial writes.
func (s *Service) CommitManagedAgentCodeSessionActivation(
	ctx context.Context,
	codeSession db.CodeSession,
	queueItems []db.SessionEventQueueItem,
	inboundInputs []db.AppendCodeSessionEventInput,
) (committed bool, err error) {
	if s == nil || s.db == nil {
		return false, db.ErrNotFound
	}
	err = s.db.WithManagedAgentActivationTx(ctx, func(tx db.ManagedAgentActivationTx) error {
		session, err := tx.LockSessionForEvents(
			ctx,
			codeSession.WorkspaceUUID,
			codeSession.SessionExternalID,
		)
		if err != nil {
			return err
		}
		codeSession, err := tx.LockInitializingCodeSession(ctx, codeSession.UUID)
		if err != nil {
			return err
		}
		if !lo.EveryBy(queueItems, func(item db.SessionEventQueueItem) bool {
			return item.Event.EventType == "user.message"
		}) {
			return db.ErrInvalidState
		}
		queueMatches, err := tx.QueueMatches(ctx, session, queueItems)
		if err != nil || !queueMatches {
			return err
		}
		for _, inbound := range inboundInputs {
			inserted, duplicate, err := tx.AppendCodeSessionInboundEvent(ctx, codeSession, inbound)
			if err != nil {
				return err
			}
			if duplicate && inserted.CodeSessionExternalID != codeSession.ExternalID {
				return db.ErrInvalidState
			}
			if !duplicate {
				codeSession.LastInboundSequenceNum = inserted.SequenceNum
			}
		}
		if err := tx.DeleteSessionEventQueue(ctx, session.UUID); err != nil {
			return err
		}
		statusUpdated, err := tx.ActivateCodeSession(ctx, codeSession.UUID, time.Now().UTC())
		if err != nil {
			return err
		}
		if !statusUpdated {
			return db.ErrInvalidState
		}
		committed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return committed, nil
}

// listAllSessionEvents returns every non-deleted public session event in
// ascending creation order by paging through ListSessionEventsPage.
func (s *Service) listAllSessionEvents(ctx context.Context, session db.Session) ([]db.SessionEvent, error) {
	var all []db.SessionEvent
	var cursor *db.SessionEventPageCursor
	for {
		page, hasMore, err := s.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceUUID:     session.WorkspaceUUID,
			SessionExternalID: session.ExternalID,
			Limit:             100,
			Cursor:            cursor,
			Order:             "asc",
		})
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if !hasMore || len(page) == 0 {
			return all, nil
		}
		last := page[len(page)-1]
		cursor = &db.SessionEventPageCursor{CreatedAt: last.CreatedAt, UUID: last.UUID}
	}
}

// convertSessionEventsToInbound keeps forwardable session events and converts
// each into an inbound write input for the given Code Session.
func (s *Service) convertSessionEventsToInbound(
	codeSessionID string,
	events []db.SessionEvent,
) ([]db.AppendCodeSessionEventInput, error) {
	inboundInputs := make([]db.AppendCodeSessionEventInput, 0, len(events))
	for _, event := range events {
		if !forwardPublicEventToWorker(event.EventType) {
			continue
		}
		inbound, err := s.convertSessionEventToInbound(codeSessionID, event)
		if err != nil {
			return nil, err
		}
		inboundInputs = append(inboundInputs, inbound)
	}
	return inboundInputs, nil
}

// convertSessionEventToInbound maps one public session event payload into a
// Code Session inbound append input.
func (s *Service) convertSessionEventToInbound(
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
