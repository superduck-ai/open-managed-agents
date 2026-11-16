package codesessions

import (
	"context"
	"encoding/json"

	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) handleToolCallResult(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.toolCallScope(w, r, true)
	if !ok {
		return
	}
	attempt, err := strconv.Atoi(r.Header.Get("Oma-Tool-Call-Attempt"))
	status := r.Header.Get("Oma-Result-Status")
	digest := r.Header.Get("Oma-Result-Sha256")
	reason := r.Header.Get("Oma-Result-Reason")
	if err != nil || attempt < 1 || !toolCallResultStatusValid(status, reason) || len(digest) != 64 || r.ContentLength < 0 {
		h.errorAdapter.Write(w, r, toolCallInputError(nil))
		return
	}
	if r.ContentLength > 8<<20 {
		h.errorAdapter.Write(w, r, toolCallInputError(&http.MaxBytesError{Limit: 8 << 20}))
		return
	}
	report := db.SandboxToolCallReport{Scope: scope, CallUUID: chi.URLParam(r, "call_uuid"), Attempt: attempt, Event: status, Reason: reason}
	preflight, err := h.db.ReportSandboxToolCall(r.Context(), db.SandboxToolCallReport{Scope: scope, CallUUID: report.CallUUID, Attempt: attempt, Event: "inspect"})
	if err != nil {
		h.errorAdapter.Write(w, r, toolCallOperationError(err))
		return
	}
	if !toolCallResultStateAccepts(preflight, digest, r.ContentLength) {
		h.errorAdapter.Write(w, r, toolCallOperationError(db.ErrToolCallConflict))
		return
	}
	if err := h.readToolCallResult(r, &report); err != nil {
		h.discardToolCallObject(r.Context(), report.ResultObjectKey)
		h.errorAdapter.Write(w, r, toolCallInputError(err))
		return
	}
	call, err := h.db.ReportSandboxToolCall(r.Context(), report)
	var audit struct {
		ObjectKey string `json:"object_key"`
	}
	_ = json.Unmarshal(call.LateResult, &audit)
	adopted := report.ResultObjectKey != nil && ((call.ResultObjectKey != nil && *call.ResultObjectKey == *report.ResultObjectKey) || audit.ObjectKey == *report.ResultObjectKey)
	if err != nil || !adopted {
		h.discardToolCallObject(r.Context(), report.ResultObjectKey)
	}
	if err == nil {
		h.notifyToolCall(call)
	}
	h.finishToolCall(w, r, call, err)
	if err == nil && call.State == "finalizing" {
		go h.finalizeToolCall(context.WithoutCancel(r.Context()), call, true)
	}
}

func toolCallResultStatusValid(status, reason string) bool {
	switch status {
	case "succeeded", "failed":
		return reason == ""
	case "interrupted":
		return reason == "stop_requested" || reason == "deadline" || reason == "executor_shutdown"
	}
	return false
}

func toolCallResultStateAccepts(call db.SandboxToolCall, digest string, size int64) bool {
	if (toolCallDone(call.State) || call.State == "finalizing") && call.State != "lost" {
		return call.ResultSHA256 != nil && *call.ResultSHA256 == digest && call.ResultBytes != nil && *call.ResultBytes == size
	}
	return true
}
