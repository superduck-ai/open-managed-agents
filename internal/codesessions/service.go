package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"

	"github.com/google/uuid"
)

// Service 封装会被 sessions、environment runner 与 code-session HTTP handler 共同复用的业务能力。
// 它不持有 HTTP 鉴权、代理连接或日志状态，因而可以安全地注入非 HTTP 调用方。
type Service struct {
	db          *db.DB
	credentials *SessionCredentials
	sinkMu      sync.Mutex
	sink        PublicEventSink
}

type workerOutputEvent struct {
	Payload   json.RawMessage
	Ephemeral bool
}

// NewService 创建只依赖持久化边界的 code-session 业务服务。
func NewService(database *db.DB) *Service {
	// 默认构造器用于无需外部配置的开发/测试调用；生产入口应显式注入稳定签发器。
	credentials, err := NewSessionCredentials(config.Config{})
	if err != nil {
		panic(err)
	}
	return NewServiceWithCredentials(database, credentials)
}

func NewServiceWithCredentials(database *db.DB, credentials *SessionCredentials) *Service {
	// 显式注入避免 Service 在同一进程中各自生成临时 Ed25519 密钥。
	if credentials == nil {
		panic("codesessions: session credentials are required")
	}
	return &Service{db: database, credentials: credentials}
}

func (s *Service) queueInitialPublicSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage, now time.Time) error {
	if len(payloads) == 0 {
		return nil
	}
	workerPayloads := make([]json.RawMessage, 0, len(payloads))
	for _, raw := range payloads {
		object, err := decodeJSONObject(raw)
		if err != nil {
			log.Printf("skip initial code session event code_session_id=%s: %v", codeSession.ExternalID, err)
			continue
		}
		if !forwardPublicEventToWorker(stringField(object, "type")) {
			continue
		}
		payload, err := workerPayloadForPublicEvent(codeSession.ExternalID, raw, now)
		if err != nil {
			log.Printf("convert initial code session event code_session_id=%s: %v", codeSession.ExternalID, err)
			continue
		}
		workerPayloads = append(workerPayloads, payload)
	}
	return s.QueueRawPublicSessionEvents(ctx, codeSession, workerPayloads)
}

func (s *Service) QueuePublicSessionEvents(ctx context.Context, session db.Session, events []db.SessionEvent) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	codeSession, err := s.db.GetCodeSessionBySessionExternalID(ctx, session.WorkspaceID, session.ExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	payloads := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		if !forwardPublicEventToWorker(event.EventType) {
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
			log.Printf("convert public session event to code session payload session_id=%s event_id=%s: %v", session.ExternalID, event.ExternalID, err)
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

func (s *Service) AppendWorkerEvent(ctx context.Context, codeSessionID string, raw json.RawMessage, source string) error {
	return s.appendWorkerEvent(ctx, codeSessionID, nil, raw, source)
}

func (s *Service) AppendWorkerEventForEpoch(ctx context.Context, codeSessionID string, workerEpoch int64, raw json.RawMessage, source string) error {
	return s.appendWorkerEvent(ctx, codeSessionID, &workerEpoch, raw, source)
}

func (s *Service) AppendWorkerOutputEventsForEpoch(ctx context.Context, codeSessionID string, workerEpoch int64, events []workerOutputEvent, source string) error {
	if s == nil || len(events) == 0 {
		return nil
	}
	codeSessionID = strings.TrimSpace(codeSessionID)
	if codeSessionID == "" {
		return ErrProtocol
	}
	if workerEpoch <= 0 {
		return db.ErrWorkerEpochMismatch
	}
	source = strings.TrimSpace(source)
	now := time.Now().UTC()
	for _, input := range events {
		payload, object, err := normalizeWorkerOutputPayload(codeSessionID, input.Payload, now)
		if err != nil {
			return err
		}
		eventType := stringField(object, "type")
		if eventType == "keep_alive" {
			if err := s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, workerEpoch); err != nil {
				return err
			}
			continue
		}
		meta, err := BuildEventMetadata(codeSessionID, "outbound", payload)
		if err != nil {
			return err
		}
		eventID, err := ids.New("csev_")
		if err != nil {
			return err
		}
		event, duplicate, err := s.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
			ExternalID:          eventID,
			EventType:           meta.EventType,
			EventSubtype:        meta.EventSubtype,
			PayloadUUID:         meta.PayloadUUID,
			RequestID:           meta.RequestID,
			Payload:             meta.Payload,
			PayloadHash:         meta.PayloadHash,
			IdempotencyKey:      meta.IdempotencyKey,
			Source:              source,
			CreatedAt:           time.Now().UTC(),
			RequiredWorkerEpoch: &workerEpoch,
			Ephemeral:           input.Ephemeral,
		})
		if err != nil {
			return err
		}
		if meta.EventType == "control_request" && meta.EventSubtype == "can_use_tool" {
			if duplicate {
				continue
			}
			if err := s.handleToolPermissionRequest(ctx, codeSessionID, object, meta); err != nil {
				return err
			}
			continue
		}
		publicObject := object
		if duplicate {
			publicObject, err = decodeJSONObject(event.Payload)
			if err != nil {
				return err
			}
		}
		if event.Ephemeral || !publicWorkerOutputEvent(event.EventType) {
			continue
		}
		publicPayloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, event, publicObject)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := s.publishPublicPayloads(ctx, codeSessionID, publicPayloads); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) appendWorkerEvent(ctx context.Context, codeSessionID string, requiredWorkerEpoch *int64, raw json.RawMessage, source string) error {
	if s == nil {
		return nil
	}
	codeSessionID = strings.TrimSpace(codeSessionID)
	if codeSessionID == "" {
		return ErrProtocol
	}
	payload, object, err := normalizeWorkerOutboundPayload(codeSessionID, raw, time.Now().UTC())
	if err != nil {
		return err
	}
	meta, err := BuildEventMetadata(codeSessionID, "outbound", payload)
	if err != nil {
		return err
	}
	if meta.EventType == "keep_alive" {
		if requiredWorkerEpoch != nil {
			return s.db.TouchCodeSessionWorkerActivityForEpoch(ctx, codeSessionID, *requiredWorkerEpoch)
		}
		return s.db.TouchCodeSessionWorkerActivity(ctx, codeSessionID)
	}
	eventID, err := ids.New("csev_")
	if err != nil {
		return err
	}
	event, duplicate, err := s.db.AppendCodeSessionOutboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
		ExternalID:          eventID,
		EventType:           meta.EventType,
		EventSubtype:        meta.EventSubtype,
		PayloadUUID:         meta.PayloadUUID,
		RequestID:           meta.RequestID,
		Payload:             meta.Payload,
		PayloadHash:         meta.PayloadHash,
		IdempotencyKey:      meta.IdempotencyKey,
		Source:              strings.TrimSpace(source),
		CreatedAt:           time.Now().UTC(),
		DeliveryStatus:      "",
		RequiredWorkerEpoch: requiredWorkerEpoch,
	})
	if err != nil {
		return err
	}
	if duplicate {
		return nil
	}
	if meta.EventType == "control_request" && meta.EventSubtype == "can_use_tool" {
		return s.handleToolPermissionRequest(ctx, codeSessionID, object, meta)
	}
	if hiddenWorkerEvent(meta.EventType) {
		return nil
	}
	publicPayloads, ok, err := publicPayloadsFromWorkerEvent(codeSessionID, event, object)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.publishPublicPayloads(ctx, codeSessionID, publicPayloads)
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
	meta, err := BuildEventMetadata(codeSessionID, "inbound", payload)
	if err != nil {
		return db.CodeSessionEvent{}, false, err
	}
	eventID, err := ids.New("csev_")
	if err != nil {
		return db.CodeSessionEvent{}, false, err
	}
	return s.db.AppendCodeSessionInboundEvent(ctx, codeSessionID, db.AppendCodeSessionEventInput{
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
	})
}

func (s *Service) publishPublicPayloads(ctx context.Context, codeSessionID string, payloads []json.RawMessage) error {
	codeSession, err := s.publishPublicPayloadsToSink(ctx, codeSessionID, payloads)
	if err != nil {
		return err
	}
	if codeSession.ExternalID == "" {
		return nil
	}
	if err := s.publishSubagentInternalEvents(ctx, codeSession); err != nil {
		log.Printf("publish subagent internal events code_session_id=%s session_id=%s: %v", codeSession.ExternalID, codeSession.SessionExternalID, err)
	}
	return nil
}

func (s *Service) publishPublicPayloadsToSink(ctx context.Context, codeSessionID string, payloads []json.RawMessage) (db.CodeSession, error) {
	if len(payloads) == 0 {
		return db.CodeSession{}, nil
	}
	codeSession, err := s.db.GetCodeSession(ctx, codeSessionID)
	if err != nil {
		return db.CodeSession{}, err
	}
	s.sinkMu.Lock()
	sink := s.sink
	s.sinkMu.Unlock()
	if sink == nil {
		return codeSession, nil
	}
	return codeSession, sink.PublishCodeSessionEvents(ctx, codeSession, payloads)
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
			WorkspaceID:           codeSession.WorkspaceID,
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
	_, err = s.publishPublicPayloadsToSink(ctx, codeSession.ExternalID, payloads)
	return err
}

func (s *Service) PublishSubagentInternalEvents(ctx context.Context, codeSession db.CodeSession) error {
	if s == nil {
		return nil
	}
	return s.publishSubagentInternalEvents(ctx, codeSession)
}

func (s *Service) subagentThreadMappings(ctx context.Context, codeSession db.CodeSession) (map[string]string, error) {
	events, _, err := s.db.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
		WorkspaceID:       codeSession.WorkspaceID,
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

func forwardPublicEventToWorker(eventType string) bool {
	switch eventType {
	case "user.message", "user.interrupt", "user.tool_confirmation", "user.tool_result", "user.custom_tool_result":
		return true
	default:
		return false
	}
}

func hiddenWorkerEvent(eventType string) bool {
	switch eventType {
	case "control_request", "control_response", "control_cancel_request":
		return true
	default:
		return false
	}
}

func publicWorkerOutputEvent(eventType string) bool {
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
	sum := sha256.Sum256([]byte(strings.TrimSpace(codeSessionID) + "\x00public\x00" + strings.TrimSpace(seed)))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func derivedPrimarySessionEventID(codeSessionID, eventID, eventType string) string {
	sum := sha256.Sum256([]byte(codeSessionID + "\x00" + eventID + "\x00" + eventType + "\x00primary"))
	return "sevt_" + hex.EncodeToString(sum[:16])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
