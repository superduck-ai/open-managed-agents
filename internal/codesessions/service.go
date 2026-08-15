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
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

// Service 封装会被 sessions、environment runner 与 code-session HTTP handler 共同复用的业务能力。
// 它不持有 HTTP 鉴权、代理连接或日志状态，因而可以安全地注入非 HTTP 调用方。
type Service struct {
	db          *db.DB
	credentials *SessionCredentials
	logger      *slog.Logger
	sink        PublicEventSink
}

func NewServiceWithCredentials(database *db.DB, credentials *SessionCredentials, logger *slog.Logger) *Service {
	// 显式注入避免 Service 在同一进程中各自生成临时 Ed25519 密钥。
	if credentials == nil {
		panic("codesessions: session credentials are required")
	}
	logger = logging.LoggerOrDefault(logger)
	return &Service{db: database, credentials: credentials, logger: logger}
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
	for _, event := range events {
		if !shouldForwardPublicEventToWorker(event.EventType) {
			continue
		}
		if event.EventType == "user.tool_confirmation" {
			handled, err := s.queueControlResponseForToolConfirmation(ctx, codeSession, event)
			if err != nil {
				return err
			}
			if handled {
				continue
			}
		}
		payload, err := workerPayloadForPublicEvent(codeSession.ExternalID, event.Payload, event.ProcessedAt)
		if err != nil {
			s.logger.ErrorContext(ctx, "convert public session event to code session payload", "session_id", session.ExternalID, "event_id", event.ExternalID, "error", err)
			continue
		}
		payloads = append(payloads, payload)
	}
	return s.QueueRawPublicSessionEvents(ctx, codeSession, payloads)
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

func (s *Service) AppendWorkerEvent(ctx context.Context, codeSessionID string, raw json.RawMessage) error {
	if s == nil {
		return nil
	}
	prepared, err := prepareSingleWorkerEvent(codeSessionID, raw, time.Now().UTC())
	if err != nil {
		return err
	}
	if prepared.eventType == "keep_alive" {
		return s.db.TouchCodeSessionWorkerActivity(ctx, codeSessionID)
	}
	return s.applyWorkerOutputEvent(ctx, codeSessionID, &prepared)
}

func (s *Service) AppendWorkerEventForEpoch(ctx context.Context, codeSessionID string, workerEpoch int64, raw json.RawMessage) error {
	if s == nil {
		return nil
	}
	prepared, err := prepareSingleWorkerEvent(codeSessionID, raw, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, workerEpoch); err != nil {
		return err
	}
	if prepared.eventType == "keep_alive" {
		return nil
	}
	return s.applyWorkerOutputEvent(ctx, codeSessionID, &prepared)
}

func (s *Service) AppendWorkerOutputEventsForEpoch(ctx context.Context, codeSessionID string, workerEpoch int64, events []workerOutputEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
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
	return s.applyWorkerOutputEvents(ctx, codeSessionID, prepared)
}

type preparedWorkerOutputEvent struct {
	eventType      string
	metadata       EventMetadata
	controlRequest *workerControlRequestPayload
	publicPayloads []json.RawMessage
}

func prepareSingleWorkerEvent(codeSessionID string, raw json.RawMessage, now time.Time) (preparedWorkerOutputEvent, error) {
	if codeSessionID == "" {
		return preparedWorkerOutputEvent{}, ErrProtocol
	}
	payload, err := normalizeWorkerOutboundPayload(codeSessionID, raw, now)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	meta, err := BuildEventMetadata(codeSessionID, "outbound", payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	prepared := preparedWorkerOutputEvent{eventType: meta.EventType}
	if meta.EventType == "keep_alive" || isHiddenWorkerEvent(meta.EventType) {
		if meta.EventType == "control_request" && meta.EventSubtype == "can_use_tool" {
			controlRequest, err := prepareWorkerControlRequest(payload, meta)
			if err != nil {
				return preparedWorkerOutputEvent{}, fmt.Errorf("%w: invalid control_request payload", ErrProtocol)
			}
			controlRequest.eventType = meta.EventType
			return controlRequest, nil
		}
		return prepared, nil
	}
	publicPayloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, transientWorkerEvent(meta, now), payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	if ok {
		prepared.publicPayloads = publicPayloads
	}
	return prepared, nil
}

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
	header, err := decodeWorkerPayloadHeader(input.Payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	if header.Type == "keep_alive" {
		return preparedWorkerOutputEvent{}, nil
	}
	meta, err := BuildEventMetadata(codeSessionID, "outbound", input.Payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	if header.Type == "control_request" {
		return prepareWorkerControlRequest(input.Payload, meta)
	}
	if input.Ephemeral || !isPublicWorkerOutputEvent(meta.EventType) {
		return preparedWorkerOutputEvent{}, nil
	}
	publicPayloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, transientWorkerEvent(meta, now), input.Payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, err
	}
	if !ok {
		return preparedWorkerOutputEvent{}, nil
	}
	return preparedWorkerOutputEvent{publicPayloads: publicPayloads}, nil
}

func prepareWorkerControlRequest(payload json.RawMessage, meta EventMetadata) (preparedWorkerOutputEvent, error) {
	controlRequest, err := decodeWorkerControlRequestPayload(payload)
	if err != nil {
		return preparedWorkerOutputEvent{}, errors.New("payload is an invalid control_request")
	}
	if controlRequest.Request.Subtype != "can_use_tool" {
		return preparedWorkerOutputEvent{}, nil
	}
	return preparedWorkerOutputEvent{
		metadata:       meta,
		controlRequest: &controlRequest,
	}, nil
}

func (s *Service) applyWorkerOutputEvents(ctx context.Context, codeSessionID string, events []preparedWorkerOutputEvent) error {
	for i := range events {
		if err := s.applyWorkerOutputEvent(ctx, codeSessionID, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyWorkerOutputEvent(ctx context.Context, codeSessionID string, event *preparedWorkerOutputEvent) error {
	if event.controlRequest != nil {
		return s.handleToolPermissionRequest(ctx, codeSessionID, event.controlRequest, event.metadata)
	}
	if len(event.publicPayloads) == 0 {
		return nil
	}
	return s.publishWorkerPublicPayloads(ctx, codeSessionID, event.publicPayloads)
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
	requestID := "initialize_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		"uuid":       uuid.NewString(),
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

func shouldForwardPublicEventToWorker(eventType string) bool {
	switch eventType {
	case "user.message", "user.interrupt", "user.tool_confirmation", "user.tool_result", "user.custom_tool_result":
		return true
	default:
		return false
	}
}

func isHiddenWorkerEvent(eventType string) bool {
	switch eventType {
	case "control_request", "control_response", "control_cancel_request":
		return true
	default:
		return false
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
