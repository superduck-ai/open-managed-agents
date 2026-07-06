package codesessions

import (
	"bytes"
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
	"github.com/gorilla/websocket"
)

type PublicEventSink interface {
	PublishCodeSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error
}

type Service struct {
	cfg                 config.Config
	db                  *db.DB
	bridgeAuthenticator BridgeAuthenticator

	mu      sync.Mutex
	sink    PublicEventSink
	workers map[string]*workerClient

	otlpLogMu sync.Mutex
}

type ManagedAgentCreateInput struct {
	Session                    db.Session
	Environment                db.Environment
	Model                      string
	Title                      string
	WorkDir                    string
	PermissionMode             string
	DangerouslySkipPermissions bool
	Config                     json.RawMessage
	InitialEvents              []json.RawMessage
}

type ManagedAgentCreateResult struct {
	CodeSessionID   string
	PublicSessionID string
	SDKURLPath      string
}

type workerOutputEvent struct {
	Payload   json.RawMessage
	Ephemeral bool
}

type workerClient struct {
	codeSessionID string
	conn          *websocket.Conn
	mu            sync.Mutex
	closed        bool
}

func NewService(cfg config.Config, database *db.DB) *Service {
	return &Service{
		cfg:     cfg,
		db:      database,
		workers: map[string]*workerClient{},
	}
}

func (s *Service) SetPublicEventSink(sink PublicEventSink) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sink = sink
}

func (s *Service) CreateManagedAgentCodeSession(ctx context.Context, input ManagedAgentCreateInput) (ManagedAgentCreateResult, error) {
	codeSessionID, err := ids.New("cse_")
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	now := time.Now().UTC()
	configObject := rawObject(input.Config)
	metadata, err := marshalRaw(map[string]any{
		"source":                         "managed_agents_local",
		"public_session_id":              input.Session.ExternalID,
		"environment_id":                 input.Environment.ExternalID,
		"title":                          input.Title,
		"config":                         configObject,
		"dangerously_skip_permissions":   input.DangerouslySkipPermissions,
		"managed_agent_session_work_dir": strings.TrimSpace(input.WorkDir),
	})
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
		Status:                "active",
		Metadata:              metadata,
		CreatedAt:             now,
	})
	if err != nil {
		return ManagedAgentCreateResult{}, err
	}
	if err := s.queueInitialize(ctx, record, input.Config, now); err != nil {
		return ManagedAgentCreateResult{}, err
	}
	if err := s.queueInitialPublicSessionEvents(ctx, record, input.InitialEvents, now); err != nil {
		return ManagedAgentCreateResult{}, err
	}
	return ManagedAgentCreateResult{
		CodeSessionID:   record.ExternalID,
		PublicSessionID: record.SessionExternalID,
		SDKURLPath:      "/v1/code/sessions/" + record.ExternalID,
	}, nil
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
	for _, payload := range payloads {
		event, duplicate, err := s.appendInboundPayload(ctx, codeSession.ExternalID, payload, "public-session")
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}
		s.pushInboundEvent(ctx, event)
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
	for _, payload := range payloads {
		event, duplicate, err := s.appendInboundPayload(ctx, codeSession.ExternalID, payload, source)
		if err != nil {
			return err
		}
		if duplicate {
			continue
		}
		s.pushInboundEvent(ctx, event)
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
	s.mu.Lock()
	sink := s.sink
	s.mu.Unlock()
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

func (s *Service) pushInboundEvent(ctx context.Context, event db.CodeSessionEvent) {
	client := s.workerFor(event.CodeSessionExternalID)
	if client == nil {
		return
	}
	if err := client.send(event.Payload); err != nil {
		log.Printf("send code session inbound event code_session_id=%s event_id=%s: %v", event.CodeSessionExternalID, event.ExternalID, err)
		s.clearWorker(event.CodeSessionExternalID, client)
		return
	}
	if err := s.db.MarkCodeSessionInboundEventSent(ctx, event.ExternalID); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("mark code session inbound event sent code_session_id=%s event_id=%s: %v", event.CodeSessionExternalID, event.ExternalID, err)
	}
}

func (s *Service) workerFor(codeSessionID string) *workerClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workers[codeSessionID]
}

func (s *Service) replaceWorker(codeSessionID string, next *workerClient) {
	s.mu.Lock()
	old := s.workers[codeSessionID]
	s.workers[codeSessionID] = next
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (s *Service) clearWorker(codeSessionID string, client *workerClient) {
	s.mu.Lock()
	if s.workers[codeSessionID] == client {
		delete(s.workers, codeSessionID)
	}
	s.mu.Unlock()
	if client != nil {
		client.close()
	}
}

func (c *workerClient) send(payload json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return websocket.ErrCloseSent
	}
	line := bytes.TrimSpace(payload)
	if len(line) == 0 {
		return nil
	}
	line = append(append([]byte(nil), line...), '\n')
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(websocket.TextMessage, line)
}

func (c *workerClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	_ = c.conn.Close()
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
