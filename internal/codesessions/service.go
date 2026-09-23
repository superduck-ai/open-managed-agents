package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

// Service 封装会被 sessions、environment runner 与 code-session HTTP handler 共同复用的业务能力。
// 它不持有 HTTP 鉴权、代理连接或日志状态，因而可以安全地注入非 HTTP 调用方。
type Service struct {
	eventPayloads          *eventpayload.Store
	db                     *db.DB
	credentials            *SessionCredentials
	logger                 *slog.Logger
	sink                   PublicEventSink
	sandboxTimeoutExtender SandboxTimeoutExtender
	sandboxTimeout         time.Duration
	workerEvents           workerevents.Broker
	workerEventAcks        workerevents.AckStore
	workerEventObjects     storage.ObjectStore
}

func NewServiceWithCredentials(database *db.DB, credentials *SessionCredentials, logger *slog.Logger) *Service {
	// 显式注入避免 Service 在同一进程中各自生成临时 Ed25519 密钥。
	if credentials == nil {
		panic("codesessions: session credentials are required")
	}
	logger = logging.LoggerOrDefault(logger)
	broker := workerevents.NewMemory()
	return &Service{
		eventPayloads: eventpayload.New(database, nil),
		db:            database, credentials: credentials, logger: logger,
		workerEvents: broker, workerEventAcks: workerevents.NewMemoryAcknowledgementStore(),
	}
}

// WithWorkerEventBroker wires the live transport copy of durable inbound events.
func (s *Service) WithWorkerEventBroker(broker workerevents.Broker) *Service {
	if s != nil && broker != nil {
		s.workerEvents = broker
	}
	return s
}

func (s *Service) WithWorkerEventState(acks workerevents.AckStore, objects storage.ObjectStore) *Service {
	if s == nil {
		return s
	}
	if acks != nil {
		s.workerEventAcks = acks
	}
	s.workerEventObjects = objects
	s.eventPayloads = eventpayload.New(s.db, objects)
	return s
}

// WithSandboxTimeoutExtender wires the provider lifecycle operation used to
// resume a paused sandbox when new public Session events are queued.
func (s *Service) WithSandboxTimeoutExtender(extender SandboxTimeoutExtender, timeout time.Duration) *Service {
	if s == nil {
		return s
	}
	s.sandboxTimeoutExtender = extender
	s.sandboxTimeout = timeout
	return s
}

func (s *Service) QueuePublicSessionEvents(ctx context.Context, session db.Session, events []db.SessionEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	codeSession, err := s.db.GetCodeSessionBySessionExternalID(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if codeSession.Status != "active" {
		return nil
	}
	payloads := make([]json.RawMessage, 0, len(events))
	queued := false
	for _, event := range events {
		if !maevents.IsPublicWorkerInputEvent(event.EventType) {
			continue
		}
		handled := false
		var controlErr error
		switch event.EventType {
		case "user.tool_confirmation":
			handled, controlErr = s.queueControlResponseForToolConfirmation(ctx, codeSession, event)
		case "user.custom_tool_result":
			handled, controlErr = s.queueControlResponseForCustomToolResult(ctx, codeSession, event)
		}
		if controlErr != nil {
			return controlErr
		}
		if handled {
			queued = true
			continue
		}
		payload, err := workerPayloadForPublicEvent(codeSession.ExternalID, event.Payload, event.UUID, event.ProcessedAt)
		if err != nil {
			return fmt.Errorf("convert public session event %s: %w", event.ExternalID, err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 && !queued {
		return nil
	}
	if len(payloads) > 0 {
		if err := s.QueueRawPublicSessionEvents(ctx, codeSession, payloads); err != nil {
			return err
		}
	}
	return s.resumeSandboxForCodeSession(ctx, codeSession)
}

func (s *Service) resumeSandboxForCodeSession(ctx context.Context, codeSession db.CodeSession) error {
	if s.sandboxTimeoutExtender == nil {
		return nil
	}
	sandbox, err := s.db.GetResumableEnvironmentSandboxForCodeSession(ctx, codeSession.ExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			_, err = s.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(ctx, codeSession.ExternalID, "", nil)
			return err
		}
		return err
	}
	if sandbox.ProviderSandboxID == nil || *sandbox.ProviderSandboxID == "" {
		return nil
	}
	providerSandboxID := *sandbox.ProviderSandboxID
	err = s.sandboxTimeoutExtender.SetTimeout(ctx, providerSandboxID, s.sandboxTimeout)
	if err == nil {
		_, err = s.db.ResumeCodeSessionWorkerLeaseForSandbox(
			ctx,
			codeSession.OrganizationUUID,
			codeSession.WorkspaceUUID,
			codeSession.ExternalID,
			providerSandboxID,
			codeSessionWorkerLeaseTTL,
		)
		return err
	}
	if !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
		return err
	}
	scheduled, scheduleErr := s.db.ScheduleEnvironmentSandboxRecoveryForCodeSession(
		ctx,
		codeSession.ExternalID,
		providerSandboxID,
		err,
	)
	if scheduleErr != nil {
		return fmt.Errorf("schedule replacement sandbox: %w", scheduleErr)
	}
	if scheduled {
		s.logger.InfoContext(
			ctx,
			"managed agent sandbox recovery scheduled",
			"code_session_id", codeSession.ExternalID,
			"provider_sandbox_id", providerSandboxID,
		)
	}
	return nil
}

func (s *Service) QueueRawPublicSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error {
	if s == nil || len(payloads) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, workerPublicationTimeout)
	defer cancel()
	batch := &inboundPublicationBatch{service: s}
	defer batch.cleanupUnpublished(ctx)
	for _, payload := range payloads {
		prepared, err := s.prepareInboundEvent(ctx, codeSession, payload, "public-session", "")
		if err != nil {
			return err
		}
		batch.events = append(batch.events, prepared)
	}
	return s.db.WithLockedActiveCodeSession(ctx, codeSession.ExternalID, func(db.CodeSession) error {
		return batch.publish(ctx)
	})
}

// AppendWorkerEvents is the legacy raw-payload entry point. Zero epoch retains
// legacy authentication; persistent writes still fence the captured worker epoch.
func (s *Service) AppendWorkerEvents(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, payloads []json.RawMessage) error {
	events := make([]workerOutputEvent, len(payloads))
	for i, payload := range payloads {
		events[i] = workerOutputEvent{Payload: payload}
	}
	return s.appendWorkerOutputEvents(ctx, route, workerEpoch, events)
}

func (s *Service) AppendWorkerOutputEventsForEpoch(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, events []workerOutputEvent) error {
	if workerEpoch <= 0 {
		return db.ErrWorkerEpochMismatch
	}
	return s.appendWorkerOutputEvents(ctx, route, workerEpoch, events)
}

func (s *Service) appendWorkerOutputEvents(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, events []workerOutputEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	if route.CodeSessionID == "" {
		return ErrProtocol
	}
	prepared, err := prepareWorkerOutputEvents(route.CodeSessionID, events, time.Now().UTC())
	if err != nil {
		return err
	}
	// Activity is transport telemetry, independent of the public batch. Reject
	// stale epochs before previews; durable writes recheck under the worker lock.
	if workerEpoch > 0 {
		if err := s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, route.CodeSessionID, workerEpoch); err != nil {
			return err
		}
	} else {
		for _, output := range prepared {
			if _, keepAlive := output.(preparedKeepAliveAction); keepAlive {
				if err := s.db.TouchCodeSessionWorkerActivity(ctx, route.CodeSessionID); err != nil {
					return err
				}
				break
			}
		}
	}
	return s.applyWorkerOutputEvents(ctx, route, workerEpoch, prepared)
}

// preparedWorkerOutputEvent is implemented by each prepared worker output
// variant to keep the apply path type-safe.
type preparedWorkerOutputEvent interface {
	implPreparedWorkerOutputEvent()
}

type preparedNoopAction struct{}

type preparedKeepAliveAction struct{}

type preparedStreamAction struct {
	payload json.RawMessage
}

type preparedControlAction struct {
	request  workerControlRequestPayload
	metadata EventMetadata
}

type preparedPublicAction struct {
	payloads []json.RawMessage
}

func (preparedNoopAction) implPreparedWorkerOutputEvent()      {}
func (preparedKeepAliveAction) implPreparedWorkerOutputEvent() {}
func (preparedStreamAction) implPreparedWorkerOutputEvent()    {}
func (preparedControlAction) implPreparedWorkerOutputEvent()   {}
func (preparedPublicAction) implPreparedWorkerOutputEvent()    {}

func prepareWorkerOutputEvents(codeSessionID string, events []workerOutputEvent, now time.Time) ([]preparedWorkerOutputEvent, error) {
	prepared := make([]preparedWorkerOutputEvent, 0, len(events))
	for i, event := range events {
		output, err := prepareWorkerOutputEvent(codeSessionID, event, now)
		if err != nil {
			return nil, fmt.Errorf("%w: events[%d]: %v", ErrProtocol, i, err)
		}
		prepared = append(prepared, output)
	}
	return prepared, nil
}

func prepareWorkerOutputEvent(codeSessionID string, input workerOutputEvent, now time.Time) (preparedWorkerOutputEvent, error) {
	if codeSessionID == "" {
		return nil, ErrProtocol
	}
	header, err := decodeWorkerPayloadHeader(input.Payload)
	if err != nil {
		return nil, err
	}
	if header.Type == "keep_alive" {
		return preparedKeepAliveAction{}, nil
	}
	payload, err := normalizeWorkerOutboundPayload(codeSessionID, input.Payload, now)
	if err != nil {
		return nil, err
	}
	meta, err := BuildEventMetadata(codeSessionID, "outbound", payload)
	if err != nil {
		return nil, err
	}
	if header.Type == "control_request" {
		prepared, err := prepareWorkerControlAction(payload, meta)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid control_request payload", ErrProtocol)
		}
		return prepared, nil
	}
	if input.Ephemeral {
		if meta.EventType == "stream_event" {
			return preparedStreamAction{payload: payload}, nil
		}
		return preparedNoopAction{}, nil
	}
	if !isPublicWorkerOutputEvent(meta.EventType) {
		return preparedNoopAction{}, nil
	}
	publicPayloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, transientWorkerEvent(meta, now), payload)
	if err != nil {
		return nil, err
	}
	if !ok {
		return preparedNoopAction{}, nil
	}
	return preparedPublicAction{payloads: publicPayloads}, nil
}

func prepareWorkerControlAction(payload json.RawMessage, meta EventMetadata) (preparedWorkerOutputEvent, error) {
	controlRequest, err := decodeWorkerControlRequestPayload(payload)
	if err != nil {
		return nil, errors.New("payload is an invalid control_request")
	}
	if controlRequest.Request.Subtype != "can_use_tool" {
		return preparedNoopAction{}, nil
	}
	return preparedControlAction{
		request:  controlRequest,
		metadata: meta,
	}, nil
}

func (s *Service) applyWorkerOutputEvents(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, outputs []preparedWorkerOutputEvent) error {
	var durable []preparedWorkerOutputEvent
	var previews []json.RawMessage
	for _, output := range outputs {
		switch prepared := output.(type) {
		case preparedNoopAction, preparedKeepAliveAction:
		case preparedStreamAction:
			previews = append(previews, prepared.payload)
		case preparedPublicAction, preparedControlAction:
			durable = append(durable, output)
		default:
			return fmt.Errorf("unsupported worker output event %T", output)
		}
	}
	if len(durable) > 0 {
		if err := s.commitWorkerSessionEvents(ctx, route.CodeSessionID, workerEpoch, durable, nil); err != nil {
			return err
		}
	}
	for _, preview := range previews {
		s.publishWorkerStreamPayload(ctx, route, workerEpoch, preview)
	}
	return nil
}

func (s *Service) publishWorkerStreamPayload(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, payload json.RawMessage) {
	if len(payload) == 0 || s.sink == nil {
		return
	}
	if err := s.sink.PublishCodeSessionStreamEvent(ctx, route, workerEpoch, payload); err != nil {
		s.logger.WarnContext(ctx, "publish worker stream preview", "code_session_id", route.CodeSessionID, "worker_epoch", workerEpoch, "error", err)
	}
}

func transientWorkerEvent(meta EventMetadata, createdAt time.Time) db.CodeSessionEvent {
	return db.CodeSessionEvent{
		EventType:      meta.EventType,
		EventSubtype:   meta.EventSubtype,
		PayloadUUID:    meta.PayloadUUID,
		RequestID:      meta.RequestID,
		Payload:        meta.Payload,
		PayloadHash:    meta.PayloadHash,
		IdempotencyKey: meta.IdempotencyKey,
		CreatedAt:      createdAt,
	}
}

func (s *Service) prepareInitializeEvent(
	ctx context.Context,
	codeSession db.CodeSession,
	configRaw json.RawMessage,
	now time.Time,
) (preparedInboundEvent, error) {
	configObject := rawObject(configRaw)
	requestID := "initialize_" + strings.TrimPrefix(codeSession.ExternalID, "cse_")
	request := map[string]any{
		"subtype": "initialize",
	}
	if systemPrompt := strings.TrimSpace(stringField(configObject, "system_prompt")); systemPrompt != "" {
		request["systemPrompt"] = systemPrompt
	}
	if appendSystemPrompt := strings.TrimSpace(stringField(configObject, "append_system_prompt")); appendSystemPrompt != "" {
		request["appendSystemPrompt"] = appendSystemPrompt
	}
	payload, err := marshalRaw(map[string]any{
		"type":       "control_request",
		"uuid":       controlResponseUUID(codeSession.ExternalID, "initialize"),
		"session_id": codeSession.ExternalID,
		"created_at": formatTime(now),
		"timestamp":  formatTime(now),
		"request_id": requestID,
		"request":    request,
	})
	if err != nil {
		return preparedInboundEvent{}, err
	}
	return s.prepareInboundEvent(ctx, codeSession, payload, "internal", "initialize")
}

func (s *Service) publishPreparedInboundEvent(ctx context.Context, prepared preparedInboundEvent) error {
	if err := s.workerEvents.Publish(ctx, prepared.messageID, prepared.envelope); err != nil {
		// A failed PubAck is ambiguous: JetStream may already have persisted the
		// event. Keep any referenced object until logical expiry so a redelivery
		// can still load its offloaded payload, and let the caller retry with the
		// same message ID.
		return workerEventUnavailable(err)
	}
	return nil
}

// CommitWorkerSessionEvents commits raw transcript and its public projection
// together. Delayed thread mappings replay stored transcript in the same transaction.
func (s *Service) CommitWorkerSessionEvents(ctx context.Context, codeSessionID string, workerEpoch int64, payloads []json.RawMessage, internalInputs []db.AppendCodeSessionInternalEventInput) error {
	return s.commitWorkerSessionEvents(ctx, codeSessionID, workerEpoch, []preparedWorkerOutputEvent{preparedPublicAction{payloads: payloads}}, internalInputs)
}

func (s *Service) commitWorkerSessionEvents(ctx context.Context, codeSessionID string, workerEpoch int64, outputs []preparedWorkerOutputEvent, internalInputs []db.AppendCodeSessionInternalEventInput) error {
	if s.sink == nil {
		return ErrPublicEventSinkUnavailable
	}
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	// Legacy server-side callers have no credential epoch. Modern worker
	// requests retain their original epoch through the persistence boundary.
	if workerEpoch == 0 {
		workerEpoch = codeSession.CurrentWorkerEpoch
	}
	var created []db.SessionEvent
	var repliesToSend []toolPermissionReply
	err = s.eventPayloads.WithEventTx(ctx, func(ctx context.Context, tx db.ManagedAgentEventTx) error {
		created = nil
		repliesToSend = nil
		session, err := tx.LockSessionForEvents(ctx, codeSession.WorkspaceUUID, codeSession.SessionExternalID)
		if err != nil {
			return err
		}
		if session.ArchivedAt != nil {
			return errSessionRejectsWorkerEvents
		}
		worker, err := tx.LockPublicEventWorker(ctx, session, db.SessionEventWorker{CodeSessionUUID: codeSession.UUID, Epoch: workerEpoch})
		if err != nil {
			return err
		}
		if _, err := s.eventPayloads.AppendInternalTx(ctx, tx, worker, internalInputs); err != nil {
			return err
		}
		for _, output := range outputs {
			var public []db.SessionEvent
			var replies []toolPermissionReply
			switch prepared := output.(type) {
			case preparedPublicAction:
				public, err = s.sink.AppendCodeSessionEvents(ctx, tx, session, codeSessionID, prepared.payloads)
			case preparedControlAction:
				public, replies, err = s.appendToolPermissionRequest(ctx, tx, session, worker, prepared)
			default:
				return fmt.Errorf("unsupported persistent worker output %T", output)
			}
			if err != nil {
				return err
			}
			created = append(created, public...)
			repliesToSend = append(repliesToSend, replies...)
			// Materialize after each output, preserving the same order whether
			// the worker sends one batch or several individual requests.
			subagentPayloads, err := s.subagentPublicPayloads(ctx, tx, worker)
			if err != nil {
				return err
			}
			materialized, err := s.sink.AppendCodeSessionEvents(ctx, tx, session, codeSessionID, subagentPayloads)
			if err != nil {
				return err
			}
			created = append(created, materialized...)
		}

		return nil
	})
	if err != nil {
		return err
	}
	s.sink.NotifyCodeSessionEvents(ctx, created)
	for _, reply := range repliesToSend {
		if err := s.respondToToolPermissionRequest(ctx, codeSessionID, reply.request, reply.permission, reply.source, "", ""); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) subagentPublicPayloads(ctx context.Context, tx db.ManagedAgentEventTx, codeSession db.CodeSession) ([]json.RawMessage, error) {
	threadByAgent, err := s.subagentThreadMappings(ctx, tx, codeSession)
	if err != nil || len(threadByAgent) == 0 {
		return nil, err
	}
	payloads := make([]json.RawMessage, 0, 32)
	afterSequence := int64(0)
	// ponytail: replay the existing transcript under the Session lock; add a
	// durable materialization cursor only if large histories make this costly.
	for {
		events, err := tx.ListCodeSessionInternalEventsForPublic(ctx, codeSession, afterSequence, internalEventsPageSize)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			event, err = s.eventPayloads.RestoreInternalTx(ctx, tx, event)
			if err != nil {
				return nil, err
			}
			if event.AgentID == nil {
				continue
			}
			threadID := threadByAgent[*event.AgentID]
			if threadID == "" {
				continue
			}
			eventPayloads, err := publicPayloadsFromInternalSubagentEvent(codeSession.ExternalID, event, threadID)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, eventPayloads...)
		}
		if len(events) < internalEventsPageSize {
			return payloads, nil
		}
		afterSequence = events[len(events)-1].SequenceNum
	}
}

func (s *Service) subagentThreadMappings(ctx context.Context, tx db.ManagedAgentEventTx, codeSession db.CodeSession) (map[string]string, error) {
	query := db.ListSessionEventsPageParams{
		WorkspaceUUID: codeSession.WorkspaceUUID, SessionExternalID: codeSession.SessionExternalID,
		PrimaryOnly: true, Limit: internalEventsPageSize, Order: "asc", Types: []string{"session.thread_created"},
	}
	threadByAgent := make(map[string]string)
	for {
		events, more, err := tx.ListSessionEventsPage(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			event, err = s.eventPayloads.RestorePublicTx(ctx, tx, event)
			if err != nil {
				return nil, err
			}
			var object workerThreadCreatedPayload
			if err := json.Unmarshal(event.Payload, &object); err != nil {
				return nil, fmt.Errorf("decode stored thread mapping: %w", err)
			}
			if object.SessionThreadID == "" {
				continue
			}
			for _, agentID := range []string{object.TaskID, object.AgentID, object.LegacyAgentID} {
				if agentID != "" {
					threadByAgent[agentID] = object.SessionThreadID
				}
			}
		}
		if !more {
			return threadByAgent, nil
		}
		last := events[len(events)-1]
		query.Cursor = &db.SessionEventPageCursor{ProcessedAt: last.ProcessedAt, ExternalID: last.ExternalID}
	}
}

func isPublicWorkerOutputEvent(eventType string) bool {
	return maevents.IsWorkerOutputEvent(eventType) || maevents.IsStreamDelta(eventType)
}

func rawObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return map[string]any{}
	}
	return object
}

func requestIDString(requestID *string) string {
	if requestID == nil {
		return ""
	}
	return strings.TrimSpace(*requestID)
}

func stablePublicEventID(codeSessionID, seed string) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00public\x00" + seed))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func derivedPrimarySessionEventID(codeSessionID, eventID, eventType string) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + eventID + "\x00" + eventType + "\x00primary"))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// EventPayloadStore shares durable event storage with the public session handler.
func (s *Service) EventPayloadStore() *eventpayload.Store { return s.eventPayloads }
