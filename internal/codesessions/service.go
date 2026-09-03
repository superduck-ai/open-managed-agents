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
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

// Service 封装会被 sessions、environment runner 与 code-session HTTP handler 共同复用的业务能力。
// 它不持有 HTTP 鉴权、代理连接或日志状态，因而可以安全地注入非 HTTP 调用方。
type Service struct {
	db                     *db.DB
	credentials            *SessionCredentials
	logger                 *slog.Logger
	sink                   PublicEventSink
	sandboxTimeoutExtender SandboxTimeoutExtender
	sandboxTimeout         time.Duration
}

func NewServiceWithCredentials(database *db.DB, credentials *SessionCredentials, logger *slog.Logger) *Service {
	// 显式注入避免 Service 在同一进程中各自生成临时 Ed25519 密钥。
	if credentials == nil {
		panic("codesessions: session credentials are required")
	}
	logger = logging.LoggerOrDefault(logger)
	return &Service{db: database, credentials: credentials, logger: logger}
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
		payload, err := workerPayloadForPublicEvent(codeSession.ExternalID, event.Payload, event.ProcessedAt)
		if err != nil {
			s.logger.ErrorContext(ctx, "convert public session event to code session payload", "session_id", session.ExternalID, "event_id", event.ExternalID, "error", err)
			continue
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
	// 持久化队列是事件投递边界：CCR v2 SSE 和保留的 HTTP poll 都从
	// 持久化入站队列消费事件。
	for _, payload := range payloads {
		_, duplicate, err := s.appendInboundPayload(ctx, codeSession.ExternalID, payload, "public-session")
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}
	}
	return nil
}

func (s *Service) QueueRawCodeSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage, source string) error {
	if s == nil || len(payloads) == 0 {
		return nil
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "code-session-api"
	}
	// 不得通过进程内 push 绕过持久化队列。CCR v2 投递必须校验 epoch，
	// 并且在 worker 被替换后仍可重放。
	for _, payload := range payloads {
		_, duplicate, err := s.appendInboundPayload(ctx, codeSession.ExternalID, payload, source)
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}
	}
	return nil
}

func (s *Service) AppendWorkerEvent(ctx context.Context, route CodeSessionStreamRoute, raw json.RawMessage) error {
	if s == nil {
		return nil
	}
	codeSessionID := route.CodeSessionID
	prepared, err := prepareWorkerOutputEvent(codeSessionID, workerOutputEvent{Payload: raw}, time.Now().UTC())
	if err != nil {
		return err
	}
	if _, keepAlive := prepared.(preparedKeepAliveAction); keepAlive {
		return s.db.TouchCodeSessionWorkerActivity(ctx, codeSessionID)
	}
	return s.applyWorkerOutputEvents(ctx, route, 0, []preparedWorkerOutputEvent{prepared})
}

func (s *Service) AppendWorkerEventForEpoch(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, raw json.RawMessage) error {
	if s == nil {
		return nil
	}
	codeSessionID := route.CodeSessionID
	prepared, err := prepareWorkerOutputEvent(codeSessionID, workerOutputEvent{Payload: raw}, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, workerEpoch); err != nil {
		return err
	}
	return s.applyWorkerOutputEvents(ctx, route, workerEpoch, []preparedWorkerOutputEvent{prepared})
}

func (s *Service) AppendWorkerOutputEventsForEpoch(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, events []workerOutputEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	codeSessionID := route.CodeSessionID
	if codeSessionID == "" {
		return ErrProtocol
	}
	if workerEpoch <= 0 {
		return db.ErrWorkerEpochMismatch
	}
	now := time.Now().UTC()
	prepared, err := prepareWorkerOutputEvents(codeSessionID, events, now)
	if err != nil {
		return err
	}
	// This conditional update is the batch linearization point. It serializes
	// against worker registration and rejects a worker that has lost its epoch.
	if err := s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, workerEpoch); err != nil {
		return err
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

func (s *Service) applyWorkerOutputEvents(ctx context.Context, route CodeSessionStreamRoute, workerEpoch int64, workerOutputEvents []preparedWorkerOutputEvent) error {
	for _, workerOutputEvent := range workerOutputEvents {
		if stream, ok := workerOutputEvent.(preparedStreamAction); ok {
			// Publish previews independently so an ingress batch cannot become one
			// oversized broker message or cause unrelated previews to fail together.
			s.publishWorkerStreamPayload(ctx, route, workerEpoch, stream.payload)
			continue
		}
		if err := s.applyNonStreamWorkerOutputEvent(ctx, route.CodeSessionID, workerEpoch, workerOutputEvent); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyNonStreamWorkerOutputEvent(ctx context.Context, codeSessionID string, workerEpoch int64, workerOutputEvent preparedWorkerOutputEvent) error {
	switch prepared := workerOutputEvent.(type) {
	case preparedNoopAction:
		return nil
	case preparedKeepAliveAction:
		return nil
	case preparedControlAction:
		return s.handleToolPermissionRequest(ctx, codeSessionID, workerEpoch, &prepared.request, prepared.metadata)
	case preparedPublicAction:
		return s.publishWorkerPublicPayloads(ctx, codeSessionID, prepared.payloads)
	default:
		return fmt.Errorf("unsupported non-stream worker output event %T", workerOutputEvent)
	}
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

func (s *Service) queueInitialize(ctx context.Context, codeSession db.CodeSession, configRaw json.RawMessage, now time.Time) error {
	configObject := rawObject(configRaw)
	requestID := "initialize_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
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
		"uuid":       uuid.NewV4().String(),
		"session_id": codeSession.ExternalID,
		"created_at": formatTime(now),
		"timestamp":  formatTime(now),
		"request_id": requestID,
		"request":    request,
	})
	if err != nil {
		return err
	}
	_, _, err = s.appendInboundPayload(ctx, codeSession.ExternalID, payload, "internal")
	return err
}

func (s *Service) appendInboundPayload(ctx context.Context, codeSessionID string, payload json.RawMessage, source string) (db.CodeSessionEvent, bool, error) {
	input, err := newInboundEventInput(codeSessionID, payload, source)
	if err != nil {
		return db.CodeSessionEvent{}, false, err
	}
	return s.db.AppendCodeSessionInboundEvent(ctx, codeSessionID, input)
}

func newInboundEventInput(codeSessionID string, payload json.RawMessage, source string) (db.AppendCodeSessionEventInput, error) {
	meta, err := BuildEventMetadata(codeSessionID, "inbound", payload)
	if err != nil {
		return db.AppendCodeSessionEventInput{}, err
	}
	eventID, err := ids.New("csev_")
	if err != nil {
		return db.AppendCodeSessionEventInput{}, err
	}
	return db.AppendCodeSessionEventInput{
		ExternalID:     eventID,
		EventType:      meta.EventType,
		EventSubtype:   meta.EventSubtype,
		PayloadUUID:    meta.PayloadUUID,
		RequestID:      meta.RequestID,
		Payload:        meta.Payload,
		PayloadHash:    meta.PayloadHash,
		IdempotencyKey: meta.IdempotencyKey,
		DeliveryStatus: "queued",
		Source:         strings.TrimSpace(source),
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (s *Service) publishWorkerPublicPayloads(ctx context.Context, codeSessionID string, payloads []json.RawMessage) error {
	if err := s.publishPublicPayloads(ctx, codeSessionID, payloads); err != nil {
		return err
	}
	s.reconcileSubagentEvents(ctx, codeSessionID)
	return nil
}

func (s *Service) publishPublicPayloads(ctx context.Context, codeSessionID string, payloads []json.RawMessage) error {
	if len(payloads) == 0 {
		return nil
	}
	// db 是可选依赖（如仅签发凭证的 Service）；无 DB 时无处可写公共事件。
	if s.db == nil {
		return nil
	}
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return err
	}
	if !found {
		return db.ErrNotFound
	}
	if s.sink == nil {
		return nil
	}
	return s.sink.PublishCodeSessionEvents(ctx, codeSession, payloads)
}

func (s *Service) reconcileSubagentEvents(ctx context.Context, codeSessionID string) {
	codeSession, found, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err == nil && found {
		err = s.publishSubagentInternalEvents(ctx, codeSession)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "publish subagent internal events", "code_session_id", codeSessionID, "error", err)
	}
}

func (s *Service) publishSubagentInternalEvents(ctx context.Context, codeSession db.CodeSession) error {
	threadByAgent, err := s.subagentThreadMappings(ctx, codeSession)
	if err != nil || len(threadByAgent) == 0 {
		return err
	}
	payloads := make([]json.RawMessage, 0, 32)
	afterSequence := int64(0)
	for {
		events, hasMore, err := s.db.ListCodeSessionInternalEventsPage(ctx, db.ListCodeSessionInternalEventsPageParams{
			WorkspaceUUID:         codeSession.WorkspaceUUID,
			CodeSessionExternalID: codeSession.ExternalID,
			Subagents:             true,
			AfterSequence:         afterSequence,
			Limit:                 internalEventsPageSize,
		})
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.AgentID == nil {
				continue
			}
			threadID := threadByAgent[strings.TrimSpace(*event.AgentID)]
			if threadID == "" {
				continue
			}
			eventPayloads, err := publicPayloadsFromInternalSubagentEvent(codeSession.ExternalID, event, threadID)
			if err != nil {
				return err
			}
			payloads = append(payloads, eventPayloads...)
		}
		if len(events) > 0 {
			afterSequence = events[len(events)-1].SequenceNum
		}
		if !hasMore {
			break
		}
	}
	if len(payloads) == 0 {
		return nil
	}
	return s.publishPublicPayloads(ctx, codeSession.ExternalID, payloads)
}

func (s *Service) PublishSubagentInternalEvents(ctx context.Context, codeSession db.CodeSession) error {
	if s == nil {
		return nil
	}
	return s.publishSubagentInternalEvents(ctx, codeSession)
}

func (s *Service) subagentThreadMappings(ctx context.Context, codeSession db.CodeSession) (map[string]string, error) {
	events, _, err := s.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceUUID:     codeSession.WorkspaceUUID,
		SessionExternalID: codeSession.SessionExternalID,
		PrimaryOnly:       true,
		Limit:             500,
		Order:             "asc",
		Types:             []string{"session.thread_created"},
	})
	if err != nil {
		return nil, err
	}
	threadByAgent := make(map[string]string)
	for _, event := range events {
		object := rawObject(event.Payload)
		threadID := strings.TrimSpace(stringField(object, "session_thread_id"))
		if threadID == "" {
			continue
		}
		for _, key := range []string{"task_id", "agent_id", "agentId"} {
			agentID := strings.TrimSpace(stringField(object, key))
			if agentID != "" {
				threadByAgent[agentID] = threadID
			}
		}
	}
	return threadByAgent, nil
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
