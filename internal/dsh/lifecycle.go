package dsh

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type Claim struct {
	CodeSessionID  string `json:"code_session_id"`
	WorkerEpoch    string `json:"worker_epoch"`
	SessionToken   string `json:"session_token"`
	ModelToken     string `json:"model_token"`
	ClaimExpiresAt string `json:"claim_expires_at"`
	TraceParent    string `json:"trace_parent,omitempty"`
}

type Lifecycle interface {
	Claim(context.Context, string, time.Duration) (Claim, bool, error)
	Release(context.Context, string, string, int64, string) error
	RegistrationContext(context.Context, db.DSHScope, string) (json.RawMessage, error)
	RenewSandbox(context.Context, db.DSHScope) error
	Telemetry(context.Context, db.DSHScope) (bool, *TraceExport)
}

type TraceExport struct {
	Content bool `json:"content"`
}

type WorkerDatabase interface {
	RegisterDSHWorker(context.Context, db.DSHScope) (time.Time, error)
	HeartbeatDSHWorker(context.Context, db.DSHScope) (time.Time, error)
}

type ControlHandler struct {
	logger       *slog.Logger
	database     WorkerDatabase
	lifecycle    Lifecycle
	authenticate Authenticator
	fleet        func(*http.Request) error
}

func NewControlHandler(database WorkerDatabase, lifecycle Lifecycle, authenticate Authenticator, fleet func(*http.Request) error, logger *slog.Logger) *ControlHandler {
	return &ControlHandler{logger: logging.LoggerOrDefault(logger), database: database, lifecycle: lifecycle, authenticate: authenticate, fleet: fleet}
}

func (h *ControlHandler) RegisterRoutes(router chi.Router) {
	router.Post("/claims", h.claim)
	router.Post("/claims/{code_session_id}/release", h.release)
	router.Route("/sessions/{code_session_id}", func(router chi.Router) {
		middleware := &Handler{authenticate: h.authenticate, logger: h.logger}
		router.Use(middleware.authorize)
		router.Post("/register", h.register)
		router.Post("/heartbeat", h.heartbeat)
	})
}

func (h *ControlHandler) authorizeFleet(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Header.Get("DSR-Protocol") != "1" {
		writeError(w, r, unsupportedProtocol(), nil)
		return "", false
	}
	if h.fleet(r) != nil {
		writeError(w, r, unauthorized(), nil)
		return "", false
	}
	id := strings.TrimSpace(r.Header.Get("X-Worker-ID"))
	if id == "" || len(id) > 128 {
		writeError(w, r, invalidRequest(), nil)
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	return id, true
}

func (h *ControlHandler) claim(w http.ResponseWriter, r *http.Request) {
	worker, ok := h.authorizeFleet(w, r)
	if !ok {
		return
	}
	value := int64(0)
	var err error
	if raw := r.URL.Query().Get("block_ms"); raw != "" {
		value, err = decimal(raw)
	}
	if err != nil || value > 30000 {
		writeError(w, r, invalidRequest(), nil)
		return
	}
	if _, err := decodeBody[struct{}](w, r); err != nil {
		writeError(w, r, err, nil)
		return
	}
	result, found, err := h.lifecycle.Claim(r.Context(), worker, time.Duration(value)*time.Millisecond)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, r, err, nil)
		}
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, result)
}

func (h *ControlHandler) release(w http.ResponseWriter, r *http.Request) {
	worker, ok := h.authorizeFleet(w, r)
	if !ok {
		return
	}
	input, err := decodeBody[struct {
		Epoch  string `json:"epoch"`
		Reason string `json:"reason"`
	}](w, r)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	epoch, err := decimal(input.Epoch)
	if err != nil || epoch == 0 || !validRelease(input.Reason) {
		writeError(w, r, invalidRequest(), nil)
		return
	}
	err = h.lifecycle.Release(r.Context(), chi.URLParam(r, "code_session_id"), worker, epoch, input.Reason)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, struct {
		OK bool `json:"ok"`
	}{true})
}

func validRelease(value string) bool {
	return value == "idle" || value == "shutdown" || value == "permanent" || value == "admission_failed"
}

type registrationResponse struct {
	Context              json.RawMessage `json:"context"`
	Epoch                string          `json:"epoch"`
	Protocol             string          `json:"protocol"`
	Lease                leaseConfig     `json:"lease"`
	ObservabilityEnabled bool            `json:"observability_enabled"`
	TraceExport          *TraceExport    `json:"trace_export,omitempty"`
}
type leaseConfig struct {
	TTLMillis               int `json:"ttl_ms"`
	GraceMillis             int `json:"grace_ms"`
	HeartbeatIntervalMillis int `json:"heartbeat_interval_ms"`
}

func (h *ControlHandler) register(w http.ResponseWriter, r *http.Request) {
	if _, err := decodeBody[struct{}](w, r); err != nil {
		writeError(w, r, err, nil)
		return
	}
	scope := scopeFrom(r)
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	snapshot, err := h.lifecycle.RegistrationContext(r.Context(), scope, token)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	if _, err := h.database.RegisterDSHWorker(r.Context(), scope); err != nil {
		writeError(w, r, err, nil)
		return
	}

	enabled, export := h.lifecycle.Telemetry(r.Context(), scope)
	response := registrationResponse{Context: snapshot, Epoch: strconv.FormatInt(scope.Epoch, 10), Protocol: "1", Lease: leaseConfig{20000, 10000, 6000}, ObservabilityEnabled: enabled, TraceExport: export}
	if observer, ok := h.lifecycle.(interface {
		ObserveOwnedRepositoryRegistration(context.Context, db.DSHScope, json.RawMessage, RegistrationIssue)
	}); ok {
		issued := &registrationIssueWriter{ResponseWriter: w, issue: RegistrationIssue{Complete: true}}
		writeJSON(issued, response)
		issue := issued.issue
		issue.Complete = issue.Complete && issued.wrote
		issue.ResponseEpoch, _ = strconv.ParseInt(response.Epoch, 10, 64)
		observer.ObserveOwnedRepositoryRegistration(r.Context(), scope, response.Context, issue)
		return
	}
	writeJSON(w, response)
}

func (h *ControlHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	if _, err := decodeBody[struct{}](w, r); err != nil {
		writeError(w, r, err, nil)
		return
	}
	expiry, err := h.database.HeartbeatDSHWorker(r.Context(), scopeFrom(r))
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	if err := h.lifecycle.RenewSandbox(r.Context(), scopeFrom(r)); err != nil {
		h.logger.ErrorContext(r.Context(), "renew DSR sandbox timeout", "code_session_id", scopeFrom(r).CodeSessionID, "error", err)
		writeError(w, r, sandboxRenewalUnavailable(), nil)
		return
	}
	writeJSON(w, struct {
		LeaseExpiresAt string `json:"lease_expires_at"`
	}{expiry.Format(time.RFC3339Nano)})
}
