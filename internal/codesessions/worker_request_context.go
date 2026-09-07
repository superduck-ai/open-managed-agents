package codesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

type workerRequestSource struct {
	Type              string `json:"type"`
	Subtype           string `json:"subtype"`
	UUID              string `json:"uuid"`
	ParentToolUseID   string `json:"parent_tool_use_id"`
	ModelRequestID    string `json:"model_request_id"`
	IsError           bool   `json:"is_error"`
	ContentBlockIndex *int   `json:"content_block_index"`
	Message           struct {
		ID string `json:"id"`
	} `json:"message"`
	Event struct {
		Type    string `json:"type"`
		Index   *int   `json:"index"`
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"message"`
	} `json:"event"`
}

type workerRequestInput struct {
	source   workerRequestSource
	payload  json.RawMessage
	metadata EventMetadata
	hash     string
}

func prepareWorkerRequestInput(original, payload json.RawMessage, meta EventMetadata) (*workerRequestInput, error) {
	if meta.EventType != "stream_event" && meta.EventType != "assistant" && meta.EventType != "result" && !(meta.EventType == "system" && meta.EventSubtype == "api_retry") {
		return nil, nil
	}
	var source workerRequestSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return nil, ErrProtocol
	}
	canonical, err := json.Marshal(decodeWorkerOutputValue(original))
	if err != nil {
		return nil, ErrProtocol
	}
	digest := sha256.Sum256(canonical)
	return &workerRequestInput{source: source, payload: payload, metadata: meta, hash: hex.EncodeToString(digest[:])}, nil
}

type workerRequestContext struct {
	primary string
	active  map[string]string
	known   map[string]bool
	scopes  map[string]bool
	indices map[string]int
}

type storedWorkerRequest struct {
	RequestID string `json:"_worker_model_request_id"`
	Epoch     int64  `json:"_worker_epoch"`
	SourceID  string `json:"_worker_source_event_id"`
}

func restoreWorkerRequestContext(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, worker db.CodeSession) (*workerRequestContext, error) {
	primary, found, err := tx.GetPrimarySessionThread(ctx, session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, db.ErrNotFound
	}
	state := &workerRequestContext{primary: primary.ExternalID, active: map[string]string{}, known: map[string]bool{}, scopes: map[string]bool{}, indices: map[string]int{}}
	// ponytail: replay the existing Session history once per native upload;
	// use a focused boundary query if long histories make this expensive.
	events, err := tx.ListSessionEventsForActivation(ctx, session)
	if err != nil {
		return nil, err
	}
	if err := state.restore(worker, events); err != nil {
		return nil, err
	}
	requestIDs := make([]string, 0, len(state.active))
	for _, requestID := range state.active {
		requestIDs = append(requestIDs, requestID)
	}
	indices, err := tx.LatestWorkerRequestBlockIndices(ctx, worker, requestIDs)
	if err != nil {
		return nil, err
	}
	for _, index := range indices {
		state.indices[index.RequestID] = index.Index
	}
	return state, nil
}

func (s *workerRequestContext) restore(worker db.CodeSession, events []db.SessionEvent) error {
	slices.SortFunc(events, func(a, b db.SessionEvent) int {
		if order := a.ProcessedAt.Compare(b.ProcessedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ExternalID, b.ExternalID)
	})
	for _, event := range events {
		if event.EventType != "span.model_request_start" && event.EventType != "span.model_request_end" {
			continue
		}
		var boundary storedWorkerRequest
		if err := json.Unmarshal(event.Payload, &boundary); err != nil {
			return err
		}
		phase := "start"
		if event.EventType == "span.model_request_end" {
			phase = "end"
		}
		if boundary.RequestID == "" || boundary.Epoch != worker.CurrentWorkerEpoch || event.ExternalID != maevents.ModelRequestEventID(worker.ExternalID, boundary.RequestID, phase) {
			continue
		}
		scope := s.primary
		if event.ThreadExternalID != nil {
			scope = *event.ThreadExternalID
		}
		s.known[boundary.RequestID] = true
		s.scopes[scope] = true
		if phase == "start" {
			s.active[scope] = boundary.RequestID
		} else if s.active[scope] == boundary.RequestID {
			delete(s.active, scope)
		}
	}
	return nil
}

func (s *workerRequestContext) scope(codeSessionID string, source workerRequestSource) string {
	if source.ParentToolUseID != "" {
		return maevents.ClaudeTaskThreadID(codeSessionID, source.ParentToolUseID)
	}
	return s.primary
}

// Receipts arbitrate before interpreting a frame. A timed-out older handler can
// otherwise acquire the lock after its retry has already advanced the request.
func (s *Service) applyWorkerRequestInput(ctx context.Context, tx db.ManagedAgentEventTx, session db.Session, worker db.CodeSession, state *workerRequestContext, input *workerRequestInput) ([]db.SessionEvent, json.RawMessage, error) {
	previous, found, err := tx.FindWorkerEventReceipt(ctx, worker, input.source.UUID)
	if err != nil {
		return nil, nil, err
	}
	if found {
		if previous.Hash != input.hash {
			return nil, nil, db.ErrSessionEventConflict
		}
		return nil, nil, nil
	}
	scope := state.scope(worker.ExternalID, input.source)
	payloads, preview, requestID, err := state.apply(worker, input, scope)
	if err != nil {
		return nil, nil, err
	}
	created, err := s.sink.AppendCodeSessionEvents(ctx, tx, session, worker.ExternalID, payloads)
	if err != nil {
		return nil, nil, err
	}
	var blockIndex *int
	if input.source.Type == "stream_event" && input.source.Event.Type == "content_block_start" {
		blockIndex = input.source.Event.Index
	} else if input.source.Type == "assistant" {
		blockIndex = input.source.ContentBlockIndex
		if index, ok := state.indices[requestID]; blockIndex == nil && ok {
			blockIndex = new(index)
		}
	}
	if err := tx.InsertWorkerEventReceipt(ctx, worker, db.WorkerEventReceipt{EventID: input.source.UUID, Scope: scope, RequestID: requestID, Hash: input.hash, BlockIndex: blockIndex}); err != nil {
		return nil, nil, err
	}
	return created, preview, nil
}

func (s *workerRequestContext) apply(worker db.CodeSession, input *workerRequestInput, scope string) ([]json.RawMessage, json.RawMessage, string, error) {
	source := input.source
	requestID := s.active[scope]
	var boundaries []json.RawMessage
	if source.Type == "stream_event" && source.Event.Type == "message_start" && source.ModelRequestID == "" {
		var err error
		boundaries, requestID, err = s.openRequest(worker, source, scope)
		if err != nil || len(boundaries) == 0 {
			return boundaries, nil, requestID, err
		}
	}
	if source.ModelRequestID != "" {
		requestID = source.ModelRequestID
	}
	if source.Type == "assistant" {
		requestID = source.Message.ID
	}
	if source.Type == "stream_event" && source.Event.Type == "content_block_start" && source.Event.Index != nil && requestID != "" {
		s.indices[requestID] = *source.Event.Index
	}
	if source.closesRequest() && requestID != "" && s.active[scope] == requestID {
		boundary, err := workerRequestBoundary(worker, source, requestID, "end", source.IsError || source.Subtype == "api_retry")
		if err != nil {
			return nil, nil, "", err
		}
		boundaries = append(boundaries, boundary...)
		delete(s.active, scope)
	}
	if source.Type == "stream_event" {
		if requestID == "" {
			return boundaries, nil, "", nil
		}
		fields, err := decodeRawJSONObject(input.payload)
		if err != nil {
			return nil, nil, "", err
		}
		setRawJSONField(fields, "model_request_id", requestID)
		preview, err := marshalRaw(fields)
		return boundaries, preview, requestID, err
	}
	fields, err := decodeRawJSONObject(input.payload)
	if err != nil {
		return nil, nil, "", err
	}
	if source.Type == "result" && s.scopes[scope] {
		setRawJSONField(fields, "model_request_events", true)
	}
	if source.ParentToolUseID != "" {
		if source.Type == "result" {
			// Thread lifecycle events belong to the primary stream; this field
			// identifies the affected child without changing the event owner.
			setRawJSONField(fields, "session_thread_id", scope)
		} else {
			setRawJSONField(fields, "_owner_session_thread_id", scope)
		}
	}
	if source.Type == "assistant" && source.ContentBlockIndex == nil {
		if index, ok := s.indices[requestID]; ok {
			setRawJSONField(fields, "content_block_index", index)
		}
	}
	raw, err := marshalRaw(fields)
	if err != nil {
		return nil, nil, "", err
	}
	public, _, err := publicPayloadsFromWorkerEvent(worker.ExternalID, transientWorkerEvent(input.metadata, time.Now().UTC()), raw)
	return append(boundaries, public...), nil, requestID, err
}

func (s workerRequestSource) closesRequest() bool {
	return s.Type == "result" || (s.Type == "system" && s.Subtype == "api_retry") || (s.Type == "stream_event" && s.Event.Type == "message_stop")
}

func (s *workerRequestContext) openRequest(worker db.CodeSession, source workerRequestSource, scope string) ([]json.RawMessage, string, error) {
	requestID := source.Event.Message.ID
	if requestID == "" || s.known[requestID] {
		return nil, requestID, nil
	}
	var boundaries []json.RawMessage
	if old := s.active[scope]; old != "" {
		ended, err := workerRequestBoundary(worker, source, old, "end", true)
		if err != nil {
			return nil, "", err
		}
		boundaries = append(boundaries, ended...)
	}
	boundary, err := workerRequestBoundary(worker, source, requestID, "start", false)
	if err != nil {
		return nil, "", err
	}
	boundaries = append(boundaries, boundary...)
	s.active[scope] = requestID
	s.known[requestID] = true
	s.scopes[scope] = true
	return boundaries, requestID, nil
}

func preparedWorkerRequestInput(output preparedWorkerOutputEvent) *workerRequestInput {
	switch prepared := output.(type) {
	case preparedStreamAction:
		return prepared.request
	case preparedPublicAction:
		return prepared.request
	default:
		return nil
	}
}

func workerRequestBoundary(worker db.CodeSession, source workerRequestSource, requestID, phase string, isError bool) ([]json.RawMessage, error) {
	payload := map[string]any{
		"type": "system", "subtype": "model_request_" + phase,
		"uuid":             maevents.ModelRequestEventID(worker.ExternalID, requestID, phase),
		"model_request_id": requestID, "parent_tool_use_id": source.ParentToolUseID,
		"_worker_epoch": worker.CurrentWorkerEpoch, "_worker_source_event_id": source.UUID,
	}
	if phase == "start" && source.Event.Message.Model != "" {
		payload["model"] = source.Event.Message.Model
	}
	if phase == "end" {
		payload["is_error"] = isError
	}
	raw, err := marshalRaw(payload)
	if err != nil {
		return nil, err
	}
	meta, err := BuildEventMetadata(worker.ExternalID, "outbound", raw)
	if err != nil {
		return nil, err
	}
	public, _, err := publicPayloadsFromWorkerEvent(worker.ExternalID, transientWorkerEvent(meta, time.Now().UTC()), raw)
	return public, err
}
