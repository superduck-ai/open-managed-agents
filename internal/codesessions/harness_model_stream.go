package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

type HarnessModelFacts struct {
	EndedAt           time.Time         `json:"ended_at"`
	Usage             ModelRequestUsage `json:"usage"`
	UpstreamRequestID string            `json:"upstream_request_id,omitempty"`
	ErrorType         string            `json:"error_type,omitempty"`
}

const (
	HarnessModelOpen     = "open"
	HarnessThinkingStart = "thinking_start"
	HarnessThinkingEnd   = "thinking_end"
	HarnessTextDelta     = "text_delta"
	HarnessModelFinish   = "finish"
)

type HarnessModelFrame struct {
	Type   string             `json:"type"`
	Index  int                `json:"index,omitempty"`
	Text   string             `json:"text,omitempty"`
	Result *HarnessModelFinal `json:"result,omitempty"`
}

type HarnessModelBlock struct {
	Index    int    `json:"index"`
	Thinking bool   `json:"thinking,omitempty"`
	Text     string `json:"text,omitempty"`
}

type HarnessModelNext string

const (
	HarnessModelDone       HarnessModelNext = "done"
	HarnessModelTool       HarnessModelNext = "tool"
	HarnessModelUserChoice HarnessModelNext = "user_choice"
)

type HarnessModelFinal struct {
	Facts  HarnessModelFacts   `json:"facts"`
	Blocks []HarnessModelBlock `json:"blocks,omitempty"`
	Call   harness.ToolCall    `json:"call,omitzero"`
	Next   HarnessModelNext    `json:"next,omitempty"`
}

func (h *Handler) handleHarnessModelStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credential, err := h.db.GetCodeSessionByOAuthAccessTokenHash(ctx, auth.HashAPIKey(auth.ExtractAPIKey(r)))
	if err != nil || credential.ExecutionRuntime != sessioncontract.CloudHarnessRuntime ||
		credential.CodeSessionExternalID != chi.URLParam(r, "code_session_id") {
		h.errorAdapter.Write(w, r, sessionIngressTokenInvalid(err))
		return
	}
	turn, err := uuid.Parse(r.Header.Get(sessioncontract.HarnessTurnHeader))
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallInputError(err))
		return
	}
	lease := db.HarnessTurnLease{
		Ref: db.HarnessTurnRef{UUID: turn.String(), OrganizationUUID: credential.OrganizationUUID,
			WorkspaceUUID: credential.WorkspaceUUID, SessionUUID: credential.PublicSessionUUID},
		CodeSessionUUID: credential.CodeSessionUUID, Epoch: credential.WorkerEpoch,
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxIngressBodySize))
	if err := h.consumeHarnessModelStream(ctx, decoder, lease, credential.PublicSessionExternalID, credential.CodeSessionExternalID); err != nil {
		if errors.Is(err, ErrProtocol) {
			h.errorAdapter.Write(w, r, toolCallInputError(err))
			return
		}
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) consumeHarnessModelStream(ctx context.Context, decoder *json.Decoder, lease db.HarnessTurnLease, sessionID, codeSessionID string) error {
	var frame HarnessModelFrame
	if err := decoder.Decode(&frame); err != nil {
		return errors.Join(ErrProtocol, err)
	}
	if frame.Type != HarnessModelOpen {
		return ErrProtocol
	}
	request, err := h.service.resumeHarnessModelRequest(ctx, lease, sessionID, codeSessionID)
	if err != nil {
		return err
	}
	finished := false
	defer func() {
		if !finished {
			h.service.abortHarnessModelStream(ctx, request)
		}
	}()
	for !finished {
		frame = HarnessModelFrame{}
		if err := decoder.Decode(&frame); err != nil {
			return errors.Join(ErrProtocol, err)
		}
		if err := h.service.applyHarnessModelFrame(ctx, request, frame); err != nil {
			return err
		}
		finished = frame.Type == HarnessModelFinish
	}
	return nil
}

func (s *Service) resumeHarnessModelRequest(ctx context.Context, lease db.HarnessTurnLease, sessionID, codeSessionID string) (*ModelRequest, error) {
	code, err := s.db.GetCodeSessionBySessionExternalID(ctx, lease.Ref.WorkspaceUUID, sessionID)
	if err != nil {
		return nil, err
	}
	if code.ExternalID != codeSessionID || code.Status != "active" {
		return nil, db.ErrWorkerEpochMismatch
	}
	startID := maevents.HarnessModelRequestID(codeSessionID, lease.Ref.UUID, lease.Epoch)
	start, err := s.db.GetSessionEvent(ctx, lease.Ref.WorkspaceUUID, sessionID, startID)
	if err != nil {
		return nil, err
	}
	var event modelRequestEvent
	if err := json.Unmarshal(start.Payload, &event); err != nil {
		return nil, err
	}
	return s.modelRequest(ctx, code, "", event.Model, &lease)
}

func (s *Service) applyHarnessModelFrame(ctx context.Context, request *ModelRequest, frame HarnessModelFrame) error {
	if frame.Index < 0 || frame.Index >= 4096 {
		return ErrProtocol
	}
	switch frame.Type {
	case HarnessThinkingStart:
		return s.StartModelThinking(ctx, request, request.StartID, frame.Index)
	case HarnessThinkingEnd:
		if err := s.EndModelThinking(ctx, request, request.StartID, frame.Index); err != nil && !errors.Is(err, db.ErrWorkerEpochMismatch) {
			s.logger.WarnContext(ctx, "publish completed thinking block", "model_request_start_id", request.StartID, "error", err)
		}
	case HarnessTextDelta:
		s.PublishHarnessModelText(ctx, request, request.StartID, frame.Index, frame.Text)
	case HarnessModelFinish:
		if frame.Result == nil || frame.Result.Facts.EndedAt.IsZero() || (frame.Result.Facts.ErrorType == "" && frame.Result.Next == "") {
			return ErrProtocol
		}
		return s.EndModelRequest(ctx, request, frame.Result.requestResult(request))
	default:
		return ErrProtocol
	}
	return nil
}

func (s *Service) abortHarnessModelStream(ctx context.Context, request *ModelRequest) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	result := ModelRequestResult{EndedAt: time.Now().UTC().Truncate(time.Microsecond), ErrorType: "incomplete_response"}
	if err := s.EndModelRequest(ctx, request, result); err != nil && !errors.Is(err, db.ErrWorkerEpochMismatch) {
		s.logger.WarnContext(ctx, "close interrupted harness model stream", "model_request_start_id", request.StartID, "error", err)
	}
}

func (f HarnessModelFinal) requestResult(request *ModelRequest) ModelRequestResult {
	result := ModelRequestResult{
		EndedAt: f.Facts.EndedAt, Usage: f.Facts.Usage,
		UpstreamRequestID: f.Facts.UpstreamRequestID, ErrorType: f.Facts.ErrorType,
		harnessNext: f.Next,
	}
	if f.Call.ID != "" {
		id := string(f.Call.ID)
		result.ToolUseIDs = []string{id}
		result.ToolUses = []ModelRequestToolUse{{ID: id, Name: f.Call.Name, Input: json.RawMessage(f.Call.Arguments)}}
	}
	for _, block := range f.Blocks {
		message := ModelRequestMessage{Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: &block.Text}}}
		if block.Thinking {
			message.Type, message.Content = "agent.thinking", nil
		}
		message.ID = maevents.StableAssistantEventID(request.CodeSessionID, request.StartID, block.Index, message.Type)
		result.Messages = append(result.Messages, message)
	}
	return result
}
