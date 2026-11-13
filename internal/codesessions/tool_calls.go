package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type toolCallSubmission struct {
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input"`
	TimeoutMS int64           `json:"timeout_ms"`
}

type toolCallResponse struct {
	CallUUID     string          `json:"call_uuid"`
	Attempt      int             `json:"attempt"`
	Tool         string          `json:"tool"`
	Input        json.RawMessage `json:"input,omitempty"`
	State        string          `json:"state"`
	DeadlineAt   time.Time       `json:"deadline_at"`
	CancelReason *string         `json:"cancel_reason,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
}

func toolCallReply(call db.SandboxToolCall) toolCallResponse {
	return toolCallResponse{CallUUID: call.UUID, Attempt: call.Attempt, Tool: call.ToolName, State: call.State, DeadlineAt: call.DeadlineAt, CancelReason: call.CancelReason, Result: call.Result}
}

func toolCallDone(state string) bool {
	switch state {
	case "succeeded", "failed", "cancelled", "expired", "lost":
		return true
	}
	return false
}

func writeToolCallJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func (h *Handler) registerToolCallRoutes(router chi.Router) {
	router.Route("/code/sandbox/tool-calls", func(r chi.Router) {
		r.Post("/claims", h.handleToolCallClaims)
		r.Post("/{call_uuid}/events", h.handleToolCallEvent)
		r.Put("/{call_uuid}/result", h.handleToolCallResult)
	})
}

func (h *Handler) toolCallScope(w http.ResponseWriter, r *http.Request, sandbox bool) (db.SandboxToolCallScope, bool) {
	var claims SessionCredentialClaims
	var err error
	if sandbox {
		if callUUID := chi.URLParam(r, "call_uuid"); callUUID != "" {
			if _, err := uuid.Parse(callUUID); err != nil {
				h.errorAdapter.Write(w, r, toolCallInputError(err))
				return db.SandboxToolCallScope{}, false
			}
		}
		claims, err = h.service.authenticateSandboxRuntime(r.Context(), auth.ExtractAPIKey(r), "")
		if err != nil {
			h.errorAdapter.Write(w, r, toolCallAuthenticationError(err))
			return db.SandboxToolCallScope{}, false
		}
	} else {
		var ok bool
		claims, ok = h.authorizeSessionIngressClaims(w, r, chi.URLParam(r, "code_session_id"))
		if !ok {
			return db.SandboxToolCallScope{}, false
		}
	}
	threadID, callID, err := toolCallPathParams(r)
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallInputError(err))
		return db.SandboxToolCallScope{}, false
	}
	return db.SandboxToolCallScope{OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID, CodeSessionID: claims.SessionID, WorkerEpoch: claims.WorkerEpoch, SandboxUUID: claims.SandboxUUID, ThreadID: threadID, CallID: callID}, true
}

func toolCallPathParams(r *http.Request) (string, string, error) {
	threadID, callID := chi.URLParam(r, "thread_id"), chi.URLParam(r, "call_id")
	if r.URL.RawPath == "" {
		return threadID, callID, nil
	}
	threadID, err := url.PathUnescape(threadID)
	if err != nil {
		return "", "", err
	}
	callID, err = url.PathUnescape(callID)
	return threadID, callID, err
}

const toolCallQueueTimeout = 15 * time.Minute

func (h *Handler) handleToolCallSubmit(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, false)
	if !ok {
		return
	}
	var request toolCallSubmission
	if err := decodeToolCallRequest(w, r, 1<<20, &request); err != nil {
		h.errorAdapter.Write(w, r, toolCallInputError(err))
		return
	}
	request.CallID, request.Tool = strings.TrimSpace(request.CallID), strings.TrimSpace(request.Tool)
	if request.CallID == "" || scope.ThreadID == "" || !json.Valid(request.Input) {
		h.errorAdapter.Write(w, r, toolCallInputError(nil))
		return
	}
	scope.CallID = request.CallID
	if _, err := h.db.GetSandboxToolCall(r.Context(), scope); err == nil {
		call, err := h.db.SubmitSandboxToolCall(r.Context(), db.SandboxToolCallSubmission{Scope: scope, Tool: request.Tool, Input: request.Input})
		if err == nil {
			h.notifyToolCall(call)
		}
		h.finishToolCall(w, r, call, err)
		return
	} else if !errors.Is(err, db.ErrNotFound) {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	session, found, err := h.db.GetScopedCodeSession(r.Context(), scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionID)
	if err == nil && !found {
		err = db.ErrNotFound
	}
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	thread := scope.ThreadID
	if thread == "root" {
		thread = ""
	}
	policies, err := h.service.currentRuntimeToolPolicies(r.Context(), session, thread)
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	allowed := map[string]bool{"bash": true, "read": true, "write": true, "edit": true, "grep": true, "glob": true}
	policy, known := policies[request.Tool]
	if !allowed[request.Tool] || (policies != nil && (!known || policy == string(resolvedToolPermissionDeny))) {
		h.errorAdapter.Write(w, r, sandboxToolDenied())
		return
	}
	if err := h.ensureSessionSandbox(r.Context(), session, true); err != nil {
		h.errorAdapter.Write(w, r, sandboxOperationError(err))
		return
	}
	timeout := request.TimeoutMS
	if timeout <= 0 {
		timeout = 180000
	}
	if timeout > 630000 {
		timeout = 630000
	}
	call, err := h.db.SubmitSandboxToolCall(r.Context(), db.SandboxToolCallSubmission{Scope: scope, Tool: request.Tool, Input: request.Input, QueueTimeout: toolCallQueueTimeout, Timeout: time.Duration(timeout) * time.Millisecond, Validate: func(ctx context.Context) (db.SandboxToolCallValidation, error) {
		failure, finalizer, state, err := h.prepareToolCallMemory(r.WithContext(ctx), session, request)
		return db.SandboxToolCallValidation{Failure: failure, Finalizer: finalizer, FinalizerState: state, FinalizeAfter: 30 * time.Second}, err
	}})
	if err == nil {
		h.notifyToolCall(call)
	}
	h.finishToolCall(w, r, call, err)
}

func (h *Handler) finishToolCall(w http.ResponseWriter, r *http.Request, call db.SandboxToolCall, err error) {
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	if toolCallDone(call.State) {
		if err := h.restoreToolCallResult(r.Context(), &call); err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
	}
	status := http.StatusOK
	if r.Method == http.MethodPost && chi.URLParam(r, "thread_id") != "" && chi.URLParam(r, "call_id") == "" {
		status = http.StatusAccepted
	}
	writeToolCallJSON(w, status, toolCallReply(call))
}

func toolCallWait(r *http.Request, name string) time.Duration {
	ms, _ := strconv.Atoi(r.URL.Query().Get(name))
	if ms < 0 {
		ms = 0
	}
	if ms > 25000 {
		ms = 25000
	}
	return time.Duration(ms) * time.Millisecond
}

func (h *Handler) handleToolCallWait(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, false)
	if !ok {
		return
	}
	call, err := h.db.GetSandboxToolCall(r.Context(), scope)
	if err != nil {
		h.finishToolCall(w, r, call, err)
		return
	}
	streamCtx, stopStream := httpapi.StreamContext(r.Context())
	defer stopStream()
	next, closeWait := h.toolCallWake(streamCtx, []string{"oma.tc." + call.UUID + ".done"}, toolCallWait(r, "wait_ms"))
	defer closeWait()
	for {
		call, err = h.db.GetSandboxToolCall(r.Context(), scope)
		if err != nil || toolCallDone(call.State) {
			h.finishToolCall(w, r, call, err)
			return
		}
		if !next() {
			if r.Context().Err() != nil {
				return
			}
			call, err = h.db.GetSandboxToolCall(r.Context(), scope)
			h.finishToolCall(w, r, call, err)
			return
		}
	}
}

func (h *Handler) handleToolCallCancel(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, false)
	if !ok {
		return
	}
	call, err := h.db.CancelSandboxToolCall(r.Context(), scope)
	if err == nil {
		h.notifyToolCall(call)
	}
	h.finishToolCall(w, r, call, err)
}

func (h *Handler) handleToolCallClaims(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, true)
	if !ok {
		return
	}
	maxCalls := 1
	if value := r.URL.Query().Get("max"); value != "" {
		var err error
		maxCalls, err = strconv.Atoi(value)
		if err != nil || maxCalls < 0 {
			h.errorAdapter.Write(w, r, toolCallInputError(err))
			return
		}
	}
	if maxCalls > 7 {
		maxCalls = 7
	}
	session, found, err := h.db.GetScopedCodeSession(r.Context(), scope.OrganizationUUID, scope.WorkspaceUUID, scope.CodeSessionID)
	if err == nil && !found {
		err = db.ErrToolCallSandboxRetired
	}
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	streamCtx, stopStream := httpapi.StreamContext(r.Context())
	defer stopStream()
	next, closeWait := h.toolCallWake(streamCtx, []string{"oma.cs." + session.UUID + ".calls"}, toolCallWait(r, "block_ms"))
	defer closeWait()
	expired := false
	for {
		calls, err := h.db.ClaimSandboxToolCalls(r.Context(), scope, maxCalls)
		if errors.Is(err, db.ErrToolCallSandboxPreparing) {
			err = nil
		}
		if err != nil {
			h.errorAdapter.Write(w, r, toolCallOperationError(err))
			return
		}
		if len(calls) > 0 || expired {
			reply := struct {
				Calls []toolCallResponse `json:"calls"`
			}{Calls: []toolCallResponse{}}
			for _, call := range calls {
				item := toolCallReply(call)
				item.Input = call.Input
				reply.Calls = append(reply.Calls, item)
			}
			writeToolCallJSON(w, 200, reply)
			return
		}
		expired = !next()
		if r.Context().Err() != nil {
			return
		}
	}
}

func (h *Handler) handleToolCallEvent(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, true)
	if !ok {
		return
	}
	var event struct {
		Attempt int    `json:"attempt"`
		Event   string `json:"event"`
	}
	if decodeToolCallRequest(w, r, 4096, &event) != nil || (event.Event != "started" && event.Event != "heartbeat" && event.Event != "reject") {
		h.errorAdapter.Write(w, r, toolCallInputError(nil))
		return
	}
	call, err := h.db.ReportSandboxToolCall(r.Context(), db.SandboxToolCallReport{Scope: scope, CallUUID: chi.URLParam(r, "call_uuid"), Attempt: event.Attempt, Event: event.Event})
	if err == nil {
		h.notifyToolCall(call)
	}
	h.finishToolCall(w, r, call, err)
}

func decodeToolCallRequest(w http.ResponseWriter, r *http.Request, limit int64, value any) error {
	if r.ContentLength > limit {
		return &http.MaxBytesError{Limit: limit}
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return errToolCallTrailingData
}
