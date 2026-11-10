package dsh

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type ToolIdentity struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Server string `json:"server,omitempty"`
}

type PermissionRequest struct {
	InputID      string          `json:"input_id"`
	Turn         string          `json:"turn"`
	DSHSessionID string          `json:"dsh_session_id"`
	ToolUseID    string          `json:"tool_use_id"`
	Tool         ToolIdentity    `json:"tool"`
	Input        json.RawMessage `json:"input"`
	Available    *bool           `json:"available,omitempty"`
}

type PermissionCancel struct {
	DSHSessionID string `json:"dsh_session_id"`
	ToolUseID    string `json:"tool_use_id"`
	InputID      string `json:"input_id,omitempty"`
}

type PermissionResponse struct {
	RequestID string          `json:"request_id"`
	State     string          `json:"state"`
	Decision  json.RawMessage `json:"decision,omitempty"`
}

type FinishRequest struct {
	Result      string  `json:"result"`
	FailureCode *string `json:"failure_code,omitempty"`
}

type DeliveryUpdate struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type DeliveryItem struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Outcome string `json:"outcome"`
}

type DeliveryResponse struct {
	Applied int            `json:"applied"`
	Ignored int            `json:"ignored"`
	Results []DeliveryItem `json:"results"`
}

type PublishResponse struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

type InputClaim struct {
	Input             json.RawMessage `json:"input"`
	CancelRequestedAt *time.Time      `json:"cancel_requested_at,omitempty"`
}

type Execution interface {
	Claim(context.Context, db.DSHScope) (InputClaim, error)
	Stream(http.ResponseWriter, *http.Request, db.DSHScope)
	Delivery(context.Context, db.DSHScope, []DeliveryUpdate) (DeliveryResponse, error)
	Finish(context.Context, db.DSHScope, string, FinishRequest) (FinishRequest, error)
	Permission(context.Context, db.DSHScope, PermissionRequest) (PermissionResponse, error)
	CancelPermission(context.Context, db.DSHScope, string, PermissionCancel) (PermissionResponse, error)
	Publish(context.Context, db.DSHScope, []json.RawMessage) (PublishResponse, error)
}

type ExecutionHandler struct {
	execution    Execution
	authenticate Authenticator
	logger       *slog.Logger
}

func NewExecutionHandler(execution Execution, authenticate Authenticator, logger *slog.Logger) *ExecutionHandler {
	return &ExecutionHandler{execution: execution, authenticate: authenticate, logger: logger}
}

func (h *ExecutionHandler) RegisterRoutes(router chi.Router) {
	guarded := router.With(NewHandler(nil, h.authenticate, h.logger).authorize)
	const base = "/sessions/{code_session_id}"
	guarded.Get(base+"/inputs/stream", h.stream)
	guarded.Post(base+"/inputs/delivery", h.delivery)
	guarded.Post(base+"/inputs/claim", h.claim)
	guarded.Post(base+"/inputs/{input_id}/finish", h.finish)
	guarded.Post(base+"/permission-requests", h.permission)
	guarded.Post(base+"/permission-requests/{request_id}/cancel", h.cancelPermission)
	guarded.Post(base+"/public-events", h.publish)
}

func (h *ExecutionHandler) stream(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("from_sequence_num") || r.Header.Get("Last-Event-ID") != "" {
		writeError(w, r, invalidRequest(), nil)
		return
	}
	h.execution.Stream(w, r, scopeFrom(r))
}

func (h *ExecutionHandler) claim(w http.ResponseWriter, r *http.Request) {
	value, err := h.execution.Claim(r.Context(), scopeFrom(r))
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, value)
}

func (h *ExecutionHandler) delivery(w http.ResponseWriter, r *http.Request) {
	body, err := decodeBody[struct {
		Updates []DeliveryUpdate `json:"updates"`
	}](w, r)
	if err == nil && (len(body.Updates) < 1 || len(body.Updates) > 64) {
		err = invalidRequest()
	}
	seen := make(map[string]bool)
	for _, update := range body.Updates {
		if update.ID == "" || seen[update.ID] || update.Status != "received" && update.Status != "processing" && update.Status != "processed" || update.Error != "" && (update.Status != "processed" || update.Error != "unsupported_input") {
			err = invalidRequest()
			break
		}
		seen[update.ID] = true
	}
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	value, err := h.execution.Delivery(r.Context(), scopeFrom(r), body.Updates)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, value)
}

func (h *ExecutionHandler) finish(w http.ResponseWriter, r *http.Request) {
	body, err := decodeBody[FinishRequest](w, r)
	if err == nil && !validFinish(body) {
		err = invalidRequest()
	}
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	value, err := h.execution.Finish(r.Context(), scopeFrom(r), chi.URLParam(r, "input_id"), body)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, value)
}

func validFinish(value FinishRequest) bool {
	if value.Result == "success" || value.Result == "cancelled" {
		return value.FailureCode == nil
	}
	if value.Result != "failed" || value.FailureCode == nil {
		return false
	}
	switch *value.FailureCode {
	case "execution_failed", "worker_shutdown", "interrupted_recovery", "quota_exhausted":
		return true
	}
	return false
}

func PermissionID(codeSessionID, dshSessionID, toolUseID string) string {
	raw, _ := json.Marshal([]string{codeSessionID, dshSessionID, toolUseID})
	digest := sha256.Sum256(raw)
	return "dsrp_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(digest[:]))
}

func (h *ExecutionHandler) permission(w http.ResponseWriter, r *http.Request) {
	body, err := decodeBody[PermissionRequest](w, r)
	if err == nil && !validPermission(body) {
		err = invalidRequest()
	}
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	value, err := h.execution.Permission(r.Context(), scopeFrom(r), body)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	status := http.StatusOK
	if value.State == "pending" {
		status = http.StatusAccepted
	}
	httpapi.WriteJSON(w, status, value)
}

func validPermission(value PermissionRequest) bool {
	if value.InputID == "" || value.DSHSessionID == "" || value.ToolUseID == "" || value.Tool.Name == "" || len(value.Input) == 0 || value.Input[0] != '{' {
		return false
	}
	if _, err := decimal(value.Turn); err != nil {
		return false
	}
	switch value.Tool.Kind {
	case "builtin", "custom", "delegation":
		return value.Tool.Server == ""
	case "mcp":
		return value.Tool.Server != ""
	}
	return false
}

func (h *ExecutionHandler) cancelPermission(w http.ResponseWriter, r *http.Request) {
	body, err := decodeBody[PermissionCancel](w, r)
	scope := scopeFrom(r)
	id := chi.URLParam(r, "request_id")
	if err == nil && (body.DSHSessionID == "" || body.ToolUseID == "" || id != PermissionID(scope.CodeSessionID, body.DSHSessionID, body.ToolUseID)) {
		err = invalidRequest()
	}
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	value, err := h.execution.CancelPermission(r.Context(), scope, id, body)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, value)
}

func (h *ExecutionHandler) publish(w http.ResponseWriter, r *http.Request) {
	body, err := decodeBody[struct {
		Events []json.RawMessage `json:"events"`
	}](w, r)
	if err == nil && (len(body.Events) < 1 || len(body.Events) > 200) {
		err = invalidRequest()
	}
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	value, err := h.execution.Publish(r.Context(), scopeFrom(r), body.Events)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, value)
}
