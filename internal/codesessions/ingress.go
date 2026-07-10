package codesessions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	maxIngressBodySize          = 16 << 20
	maxLoggedWorkerRequestBytes = 256 << 10
	codeSessionWorkerLeaseTTL   = 60 * time.Second
	codeSessionWorkerLeaseGrace = 10 * time.Second
	internalEventsPageSize      = 500
)

type BridgeAuthenticator func(r *http.Request, codeSessionID string) (auth.Principal, *httpapi.Error)

func (s *Service) SetBridgeAuthenticator(authenticator BridgeAuthenticator) {
	if s == nil {
		return
	}
	s.bridgeAuthenticator = authenticator
}

func (s *Service) RegisterRoutes(router chi.Router) {
	router.Get("/v1/code/sessions/{code_session_id}", s.handleCodeSession)
	router.Post("/v1/code/sessions/{code_session_id}", s.handleCodeSession)
	router.Put("/v1/code/sessions/{code_session_id}", s.handleCodeSession)
	router.Put("/v1/code/sessions/{code_session_id}/worker", s.handlePutCodeSessionWorker)
	router.Get("/v1/code/sessions/{code_session_id}/worker", s.handleGetCodeSessionWorker)
	router.HandleFunc("/v1/code/sessions/{code_session_id}/worker/internal-events", s.handleCodeSessionWorkerInternalEvents)
	router.Get("/v1/code/sessions/{code_session_id}/worker/events/stream", s.handleCodeSessionWorkerEventsStream)
	router.Post("/v1/code/sessions/{code_session_id}/worker/register", s.handleCodeSessionWorkerRegister)
	router.Post("/v1/code/sessions/{code_session_id}/bridge", s.handleCodeSessionBridge)
	router.Post("/v1/code/sessions/{code_session_id}/worker/events", s.handleCodeSessionWorkerEvents)
	router.Post("/v1/code/sessions/{code_session_id}/worker/events/delivery", s.handleCodeSessionWorkerDelivery)
	router.Post("/v1/code/sessions/{code_session_id}/worker/diagnostics", s.handleCodeSessionWorkerDiagnostics)
	router.Post("/v1/code/sessions/{code_session_id}/worker/heartbeat", s.handleCodeSessionWorkerHeartbeat)
	router.Post("/v1/code/sessions/{code_session_id}/worker/otlp/metrics", s.handleCodeSessionWorkerOTLP)
	router.Post("/v1/code/sessions/{code_session_id}/worker/otlp/logs", s.handleCodeSessionWorkerOTLP)
	// Legacy HTTP session_ingress routes remain token-only for compatibility.
	// CCR v2 ownership is enforced on the /worker/* surface.
	router.Get("/v1/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Post("/v1/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Put("/v1/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Get("/v2/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Post("/v2/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Put("/v2/session_ingress/session/{code_session_id}", s.handleSessionIngressPersistence)
	router.Post("/v1/session_ingress/session/{code_session_id}/events", s.handleSessionIngressEvents)
	router.Post("/v2/session_ingress/session/{code_session_id}/events", s.handleSessionIngressEvents)
	router.Post("/v1/session_ingress/session/{code_session_id}/diag_logs", s.handleSessionIngressDiagLogs)
	router.Post("/v2/session_ingress/session/{code_session_id}/diag_logs", s.handleSessionIngressDiagLogs)
	router.Get("/v2/sessions/{code_session_id}", s.handleSessionContext)
}

func (s *Service) handleCodeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// legacy WebSocket ingress 已移除。必须在请求进入旧的 30 秒 HTTP poll
		// 之前拒绝 WebSocket upgrade，避免退化成长轮询请求。
		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
			return
		}
		s.handleCodeSessionHTTPPoll(w, r)
		return
	}
	s.handleSessionIngressPersistence(w, r)
}

func (s *Service) handleCodeSessionHTTPPoll(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, err := s.db.GetCodeSession(r.Context(), codeSessionID); err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	if err := s.db.MarkCodeSessionWorkerConnected(r.Context(), codeSessionID); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("mark code session http poll connected code_session_id=%s: %v", codeSessionID, err)
	}

	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := s.db.ListQueuedCodeSessionInboundEvents(r.Context(), codeSessionID)
		if err != nil {
			log.Printf("list queued code session http poll events code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list code session events"))
			return
		}
		if len(events) > 0 {
			payloads := make([]json.RawMessage, 0, len(events))
			for _, event := range events {
				payloads = append(payloads, event.Payload)
			}
			httpapi.WriteJSON(w, http.StatusOK, map[string]any{"events": payloads})
			for _, event := range events {
				if err := s.db.MarkCodeSessionInboundEventSent(r.Context(), event.ExternalID); err != nil && !errors.Is(err, db.ErrNotFound) {
					log.Printf("mark code session http poll event sent code_session_id=%s event_id=%s: %v", codeSessionID, event.ExternalID, err)
				}
			}
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			httpapi.WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) handlePutCodeSessionWorker(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	input, err := decodeCodeSessionWorkerStateBody(w, r)
	if err != nil {
		writeCodeSessionWorkerBodyReadError(w, r, err)
		return
	}
	updated, err := s.db.UpdateCodeSessionWorkerState(r.Context(), codeSessionID, input)
	if err != nil {
		s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not update code session worker")
		return
	}
	if input.WorkerStatus != nil {
		if err := s.syncPublicSessionStatusFromWorker(r.Context(), updated, *input.WorkerStatus); err != nil {
			log.Printf("sync public session status from worker code_session_id=%s session_id=%s worker_status=%s: %v", codeSessionID, updated.SessionExternalID, *input.WorkerStatus, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not sync code session worker status"))
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, s.codeSessionWorkerState(updated, r, input.WorkerEpoch))
}

func (s *Service) handleGetCodeSessionWorker(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, _, ok := s.validateOptionalWorkerEpochRequest(w, r, codeSessionID); !ok {
		return
	}
	record, err := s.db.GetCodeSession(r.Context(), codeSessionID)
	if err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, codeSessionWorkerReadState(record))
}

func (s *Service) handleCodeSessionWorkerInternalEvents(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	record, err := s.db.GetCodeSession(r.Context(), codeSessionID)
	if err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	if r.Method == http.MethodPost {
		body, err := readCodeSessionWorkerBody(w, r)
		if err != nil {
			logCodeSessionWorkerInternalEventsBadRequest(r, codeSessionID, nil, err)
			writeCodeSessionWorkerBodyReadError(w, r, err)
			return
		}
		epoch, err := parseRequiredWorkerEpochFromBody(body)
		if err != nil {
			logCodeSessionWorkerInternalEventsBadRequest(r, codeSessionID, body, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
			return
		}
		if !s.validateWorkerEpochValue(w, r, codeSessionID, epoch) {
			return
		}
		events, err := decodeCodeSessionWorkerInternalEventsPayload(codeSessionID, body)
		if err != nil {
			logCodeSessionWorkerInternalEventsBadRequest(r, codeSessionID, body, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
			return
		}
		created, err := s.db.AppendCodeSessionInternalEvents(r.Context(), codeSessionID, epoch, events)
		if err != nil {
			if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
				s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not append code session worker internal events")
				return
			}
			log.Printf("append code session worker internal events code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append code session worker internal events"))
			return
		}
		if len(created) > 0 {
			if err := s.publishSubagentInternalEvents(r.Context(), record); err != nil {
				log.Printf("publish subagent internal events after append code_session_id=%s session_id=%s: %v", codeSessionID, record.SessionExternalID, err)
			}
		}
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodGet {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusMethodNotAllowed, "invalid_request_error", "Method not allowed"))
		return
	}
	cursor, err := parseCodeSessionInternalEventsCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	events, hasMore, err := s.db.ListCodeSessionInternalEventsPage(r.Context(), db.ListCodeSessionInternalEventsPageParams{
		WorkspaceID:           record.WorkspaceID,
		CodeSessionExternalID: codeSessionID,
		Subagents:             strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("subagents")), "true"),
		AfterSequence:         cursor,
		Limit:                 internalEventsPageSize,
	})
	if err != nil {
		log.Printf("list code session worker internal events code_session_id=%s: %v", codeSessionID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list code session worker internal events"))
		return
	}
	data := make([]any, 0, len(events))
	for _, event := range events {
		data = append(data, codeSessionInternalEventResponse(event))
	}
	var nextCursor any
	if hasMore && len(events) > 0 {
		nextCursor = strconv.FormatInt(events[len(events)-1].SequenceNum, 10)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"data":        data,
		"next_cursor": nextCursor,
	})
}

func (s *Service) handleCodeSessionWorkerEventsStream(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, err := s.db.GetCodeSession(r.Context(), codeSessionID); err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	epoch, hasEpoch, ok := s.validateOptionalWorkerEpochRequest(w, r, codeSessionID)
	if !ok {
		return
	}
	fromSequence, err := parseCodeSessionWorkerStreamFromSequence(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Streaming is not supported"))
		return
	}
	if hasEpoch {
		if err := s.db.MarkCodeSessionWorkerConnectedForEpoch(r.Context(), codeSessionID, epoch); err != nil {
			s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not connect code session worker stream")
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.db.MarkCodeSessionWorkerDisconnectedForEpoch(ctx, codeSessionID, epoch); err != nil && !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrWorkerEpochMismatch) {
				log.Printf("mark code session worker stream disconnected code_session_id=%s: %v", codeSessionID, err)
			}
		}()
	}
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	s.streamCodeSessionWorkerEvents(r.Context(), w, flusher, codeSessionID, epoch, hasEpoch, fromSequence)
}

func (s *Service) handleCodeSessionWorkerRegister(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if err := validateCodeSessionWorkerRegisterBody(w, r, codeSessionID); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	epoch, _, err := s.db.RegisterCodeSessionWorker(r.Context(), codeSessionID, db.CodeSessionWorkerBinding{
		TokenSessionID: codeSessionID,
		AuthMode:       "session_ingress_token",
		Subject:        codeSessionID,
		Issuer:         "claude-api-server",
	}, codeSessionWorkerLeaseTTL)
	if err != nil {
		s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not register code session worker")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"worker_epoch": strconv.FormatInt(epoch, 10)})
}

func (s *Service) handleCodeSessionBridge(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	principal, ok := s.authorizeBridge(w, r, codeSessionID)
	if !ok {
		return
	}
	if err := validateCodeSessionWorkerRegisterBody(w, r, codeSessionID); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	binding, err := codeSessionBridgeWorkerBinding(codeSessionID, principal)
	if err != nil {
		log.Printf("build code session bridge binding code_session_id=%s: %v", codeSessionID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not bridge code session worker"))
		return
	}
	epoch, _, err := s.db.RegisterCodeSessionWorker(r.Context(), codeSessionID, binding, codeSessionWorkerLeaseTTL)
	if err != nil {
		s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not bridge code session worker")
		return
	}
	baseURL := s.codeSessionResponseBaseURL(r)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"worker_jwt":        codeSessionID,
		"worker_token":      codeSessionID,
		"worker_token_type": "session_ingress_token",
		"api_base_url":      baseURL,
		"expires_in":        int(codeSessionWorkerLeaseTTL / time.Second),
		"worker_epoch":      strconv.FormatInt(epoch, 10),
	})
}

func (s *Service) streamCodeSessionWorkerEvents(ctx context.Context, w io.Writer, flusher http.Flusher, codeSessionID string, epoch int64, epochScoped bool, fromSequence int64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	lastSentSequence := fromSequence
	for {
		var events []db.CodeSessionEvent
		var err error
		if epochScoped {
			events, err = s.db.ListCodeSessionInboundEventsForWorkerStream(ctx, codeSessionID, epoch, lastSentSequence)
		} else {
			events, err = s.db.ListQueuedCodeSessionInboundEvents(ctx, codeSessionID)
		}
		if err != nil {
			if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
				return
			}
			if !errors.Is(err, context.Canceled) {
				log.Printf("list queued code session worker stream events code_session_id=%s: %v", codeSessionID, err)
			}
			return
		}
		for _, event := range events {
			if err := writeCodeSessionWorkerSSEEvent(w, flusher, event); err != nil {
				log.Printf("write code session worker stream event code_session_id=%s event_id=%s: %v", codeSessionID, event.ExternalID, err)
				return
			}
			if event.SequenceNum > lastSentSequence {
				lastSentSequence = event.SequenceNum
			}
			markCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if epochScoped {
				err = s.db.MarkCodeSessionInboundEventSentForEpoch(markCtx, codeSessionID, event.ExternalID, epoch)
			} else {
				err = s.db.MarkCodeSessionInboundEventSent(markCtx, event.ExternalID)
			}
			cancel()
			if errors.Is(err, db.ErrWorkerEpochMismatch) {
				return
			}
			if err != nil && !errors.Is(err, db.ErrNotFound) {
				log.Printf("mark code session worker stream event sent code_session_id=%s event_id=%s: %v", codeSessionID, event.ExternalID, err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-keepAlive.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Service) handleCodeSessionWorkerEvents(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	workerReq, ok := s.requireWorkerEpochBody(w, r, codeSessionID)
	if !ok {
		return
	}
	events, err := decodeCodeSessionWorkerEventsPayload(workerReq.body)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	if err := s.AppendWorkerOutputEventsForEpoch(r.Context(), codeSessionID, workerReq.epoch, events, "code-session-worker"); err != nil {
		if errors.Is(err, ErrProtocol) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
			return
		}
		if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
			s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not append code session worker events")
			return
		}
		log.Printf("append code session worker events code_session_id=%s: %v", codeSessionID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append code session worker events"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleCodeSessionWorkerDelivery(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	workerReq, ok := s.requireWorkerEpochBody(w, r, codeSessionID)
	if !ok {
		return
	}
	updates, err := decodeCodeSessionWorkerDeliveryPayload(workerReq.body)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	result, err := s.db.ApplyCodeSessionWorkerDeliveryUpdates(r.Context(), codeSessionID, workerReq.epoch, updates)
	if err != nil {
		if errors.Is(err, db.ErrInvalidState) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "delivery updates are invalid"))
			return
		}
		if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
			s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not apply code session worker delivery")
			return
		}
		log.Printf("apply code session worker delivery code_session_id=%s: %v", codeSessionID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not apply code session worker delivery"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"applied": result.Applied,
		"ignored": result.Ignored,
	})
}

func (s *Service) handleCodeSessionWorkerDiagnostics(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	workerReq, ok := s.requireWorkerEpochBody(w, r, codeSessionID)
	if !ok {
		return
	}
	payloads, err := decodeDiagLogPayload(workerReq.body)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	for _, payload := range payloads {
		if err := s.AppendWorkerEventForEpoch(r.Context(), codeSessionID, workerReq.epoch, payload, "code-session-worker-diagnostics"); err != nil {
			if errors.Is(err, ErrProtocol) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
				return
			}
			if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
				s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not append code session worker diagnostics")
				return
			}
			log.Printf("append code session worker diagnostic log code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append code session worker diagnostics"))
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleCodeSessionWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	input, err := decodeCodeSessionWorkerHeartbeatBody(w, r, codeSessionID)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeCodeSessionWorkerBodyReadError(w, r, err)
			return
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	expiresAt, err := s.db.RecordCodeSessionWorkerHeartbeat(r.Context(), codeSessionID, input.WorkerEpoch, codeSessionWorkerLeaseTTL, codeSessionWorkerLeaseGrace)
	if err != nil {
		var heartbeatErr *db.CodeSessionWorkerHeartbeatError
		if errors.As(err, &heartbeatErr) && (errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrWorkerLeaseExpired)) {
			log.Printf("code session worker heartbeat rejected request_id=%s code_session_id=%s provided_epoch=%d current_epoch=%d worker_lease_expires_at=%s reason=%v",
				httpapi.RequestID(r.Context()),
				codeSessionID,
				heartbeatErr.ProvidedEpoch,
				heartbeatErr.CurrentEpoch,
				workerLeaseTimeText(heartbeatErr.WorkerLeaseExpiresAt),
				heartbeatErr.Err,
			)
		}
		if errors.Is(err, db.ErrWorkerNotRegistered) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Code session worker not registered"))
			return
		}
		if errors.Is(err, db.ErrWorkerLeaseExpired) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusGone, "session_expired", "Code session worker lease expired"))
			return
		}
		if errors.Is(err, db.ErrWorkerEpochMismatch) || errors.Is(err, db.ErrNotFound) {
			s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not record code session worker heartbeat")
			return
		}
		log.Printf("touch code session worker heartbeat code_session_id=%s: %v", codeSessionID, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not record code session worker heartbeat"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "worker_lease_expires_at": expiresAt.Format(time.RFC3339Nano)})
}

func (s *Service) handleCodeSessionWorkerOTLP(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	body, err := readCodeSessionWorkerBody(w, r)
	if err != nil {
		logCodeSessionWorkerOTLPRequest(r, codeSessionID, body, 0, false, "", "", "body_read_error", err)
		writeCodeSessionWorkerBodyReadError(w, r, err)
		return
	}
	epoch, found, epochSource, epochValue, err := parseOptionalWorkerEpochFromRequestWithSource(r)
	if err != nil {
		logCodeSessionWorkerOTLPRequest(r, codeSessionID, body, epoch, found, epochSource, epochValue, "epoch_parse_error", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	if !found {
		if err := s.db.TouchCodeSessionWorkerActivity(r.Context(), codeSessionID); err != nil {
			logCodeSessionWorkerOTLPRequest(r, codeSessionID, body, 0, false, "", "", "activity_touch_error", err)
			s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not record code session worker OTLP activity")
			return
		}
		logCodeSessionWorkerOTLPRequest(r, codeSessionID, body, 0, false, "", "", "missing_epoch_best_effort", nil)
		s.recordCodeSessionWorkerOTLP(r, codeSessionID, body, false, "", "")
		writeOTLPSuccess(w, r)
		return
	}
	if err := s.db.TouchCodeSessionWorkerActivityForEpoch(r.Context(), codeSessionID, epoch); err != nil {
		logCodeSessionWorkerOTLPRequest(r, codeSessionID, body, epoch, true, epochSource, epochValue, "epoch_activity_touch_error", err)
		s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not record code session worker OTLP activity")
		return
	}
	s.recordCodeSessionWorkerOTLP(r, codeSessionID, body, true, epochSource, epochValue)
	writeOTLPSuccess(w, r)
}

func (s *Service) handleSessionIngressEvents(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, err := s.db.GetCodeSession(r.Context(), codeSessionID); err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	payloads, err := decodeIngressEventsBody(w, r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	for _, payload := range payloads {
		if err := s.AppendWorkerEvent(r.Context(), codeSessionID, payload, "http-ingress"); err != nil {
			if errors.Is(err, ErrProtocol) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
				return
			}
			log.Printf("append code session http ingress event code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append session ingress events"))
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleSessionIngressDiagLogs(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, err := s.db.GetCodeSession(r.Context(), codeSessionID); err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	payloads, err := decodeDiagLogBody(w, r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	for _, payload := range payloads {
		if err := s.AppendWorkerEvent(r.Context(), codeSessionID, payload, "diag-logs"); err != nil {
			if errors.Is(err, ErrProtocol) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
				return
			}
			log.Printf("append code session diag log code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append session ingress diag logs"))
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleSessionIngressPersistence(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	if _, err := s.db.GetCodeSession(r.Context(), codeSessionID); err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	if r.Method == http.MethodGet {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}
	if r.Body == nil || r.ContentLength == 0 {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	payloads, err := decodeIngressEventsBody(w, r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	for _, payload := range payloads {
		if err := s.AppendWorkerEvent(r.Context(), codeSessionID, payload, "http-persistence"); err != nil {
			log.Printf("append code session persistence event code_session_id=%s: %v", codeSessionID, err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not append session ingress event"))
			return
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	if !s.authorizeIngress(w, r, codeSessionID) {
		return
	}
	record, err := s.db.GetCodeSession(r.Context(), codeSessionID)
	if err != nil {
		writeIngressLoadError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"session_context": sessionContextFromCodeSession(record),
	})
}

func sessionContextFromCodeSession(record db.CodeSession) map[string]any {
	config := codeSessionConfig(record.Metadata)
	return map[string]any{
		"cwd":                  record.WorkDir,
		"outcomes":             arrayConfigValue(config["outcomes"]),
		"custom_system_prompt": stringField(config, "custom_system_prompt"),
		"append_system_prompt": stringField(config, "append_system_prompt"),
		"model":                firstNonEmpty(record.Model, stringField(config, "model")),
		"mcp_config":           objectConfigValue(config["mcp_config"]),
	}
}

func (s *Service) codeSessionWorkerState(record db.CodeSession, r *http.Request, epoch int64) map[string]any {
	if epoch <= 0 {
		epoch = record.CurrentWorkerEpoch
	}
	epochText := strconv.FormatInt(epoch, 10)
	baseURL := s.codeSessionResponseBaseURL(r)
	return map[string]any{
		"ok":             true,
		"session_id":     record.ExternalID,
		"status":         record.ConnectionStatus,
		"worker_epoch":   epochText,
		"connection_url": strings.TrimRight(baseURL, "/") + r.URL.Path,
		"worker": map[string]any{
			"external_metadata":       rawJSONObjectValue(record.WorkerExternalMetadata),
			"internal_metadata":       nil,
			"worker_epoch":            epochText,
			"worker_status":           record.WorkerStatus,
			"requires_action_details": rawJSONValue(record.WorkerRequiresActionDetails),
		},
	}
}

func codeSessionWorkerReadState(record db.CodeSession) map[string]any {
	worker := map[string]any{}
	if metadata, ok := rawNonEmptyJSONObjectValue(record.WorkerExternalMetadata); ok {
		worker["external_metadata"] = metadata
	}
	return map[string]any{"worker": worker}
}

func rawJSONValue(raw json.RawMessage) any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	return copyRawMessage(raw)
}

func rawJSONObjectValue(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return json.RawMessage(`{}`)
	}
	return copyRawMessage(raw)
}

func rawNonEmptyJSONObjectValue(raw json.RawMessage) (json.RawMessage, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return nil, false
	}
	return copyRawMessage(raw), true
}

func (s *Service) syncPublicSessionStatusFromWorker(ctx context.Context, record db.CodeSession, workerStatus string) error {
	publicStatus, ok := publicSessionStatusFromWorkerStatus(workerStatus)
	if !ok || strings.TrimSpace(record.SessionExternalID) == "" {
		return nil
	}
	if err := s.db.SetSessionStatus(ctx, record.WorkspaceID, record.SessionExternalID, publicStatus); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	thread, err := s.db.GetPrimarySessionThread(ctx, record.WorkspaceID, record.SessionExternalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.db.SetSessionThreadStatus(ctx, record.WorkspaceID, record.SessionExternalID, thread.ExternalID, publicStatus); err != nil && !errors.Is(err, db.ErrNotFound) {
		return err
	}
	return nil
}

func publicSessionStatusFromWorkerStatus(workerStatus string) (string, bool) {
	switch workerStatus {
	case "running":
		return "running", true
	case "idle", "requires_action":
		return "idle", true
	default:
		return "", false
	}
}

func (s *Service) codeSessionResponseBaseURL(r *http.Request) string {
	if s != nil {
		if baseURL := strings.TrimSpace(s.cfg.CodeSessionAPIBaseURL); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
		if baseURL := strings.TrimSpace(s.cfg.PublicBaseURL); baseURL != "" {
			return strings.TrimRight(baseURL, "/")
		}
	}
	return codeSessionRequestBaseURL(r)
}

func codeSessionRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := strings.TrimSpace(r.Header.Get("x-forwarded-proto")); forwardedProto != "" {
		scheme = strings.Split(forwardedProto, ",")[0]
	}
	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("x-forwarded-host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	return scheme + "://" + strings.TrimSpace(host)
}

func writeCodeSessionWorkerSSEEvent(w io.Writer, flusher http.Flusher, event db.CodeSessionEvent) error {
	envelope := map[string]any{
		"event_id":     codeSessionWorkerSSEEventID(event),
		"event_type":   event.EventType,
		"payload":      event.Payload,
		"sequence_num": event.SequenceNum,
		"session_id":   event.CodeSessionExternalID,
		"type":         event.EventType,
	}
	if event.EventSubtype != "" {
		envelope["event_subtype"] = event.EventSubtype
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "id: "+strconv.FormatInt(event.SequenceNum, 10)+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "event: client_event\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "+string(data)+"\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func codeSessionWorkerSSEEventID(event db.CodeSessionEvent) string {
	if event.PayloadUUID != nil {
		if value := strings.TrimSpace(*event.PayloadUUID); value != "" {
			return value
		}
	}
	return event.ExternalID
}

func codeSessionConfig(metadata json.RawMessage) map[string]any {
	object := rawObject(metadata)
	config, ok := object["config"].(map[string]any)
	if !ok || config == nil {
		return map[string]any{}
	}
	return config
}

func arrayConfigValue(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return []any{}
	}
	return items
}

func objectConfigValue(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return map[string]any{}
	}
	return object
}

func (s *Service) authorizeIngress(w http.ResponseWriter, r *http.Request, codeSessionID string) bool {
	if strings.TrimSpace(codeSessionID) == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
		return false
	}
	token := auth.ExtractAPIKey(r)
	if token != codeSessionID {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid session ingress token"))
		return false
	}
	return true
}

func (s *Service) authorizeBridge(w http.ResponseWriter, r *http.Request, codeSessionID string) (auth.Principal, bool) {
	if strings.TrimSpace(codeSessionID) == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
		return auth.Principal{}, false
	}
	if s == nil || s.bridgeAuthenticator == nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Bridge authentication is not configured"))
		return auth.Principal{}, false
	}
	principal, err := s.bridgeAuthenticator(r, codeSessionID)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return auth.Principal{}, false
	}
	return principal, true
}

func codeSessionBridgeWorkerBinding(codeSessionID string, principal auth.Principal) (db.CodeSessionWorkerBinding, error) {
	metadata, err := marshalRaw(map[string]any{
		"credential_type":     principal.CredentialType,
		"api_key_id":          principal.APIKeyExternalID,
		"organization_id":     principal.OrganizationExternalID,
		"workspace_id":        principal.WorkspaceExternalID,
		"user_id":             principal.UserExternalID,
		"platform_session_id": principal.PlatformSessionExternalID,
		"environment_id":      principal.EnvironmentExternalID,
	})
	if err != nil {
		return db.CodeSessionWorkerBinding{}, err
	}
	return db.CodeSessionWorkerBinding{
		TokenSessionID: codeSessionID,
		AuthMode:       "bridge_bearer",
		Subject: firstNonEmpty(
			principal.UserExternalID,
			principal.APIKeyExternalID,
			principal.PlatformSessionExternalID,
			principal.WorkspaceExternalID,
			codeSessionID,
		),
		Issuer:   "claude-api-server",
		Metadata: metadata,
	}, nil
}

func writeIngressLoadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, db.ErrNotFound) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Code session not found"))
		return
	}
	log.Printf("load code session: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not load code session"))
}

type codeSessionWorkerEpochBody struct {
	body  []byte
	epoch int64
}

type codeSessionWorkerHeartbeatInput struct {
	SessionID   string
	WorkerEpoch int64
}

func (s *Service) requireWorkerEpochBody(w http.ResponseWriter, r *http.Request, codeSessionID string) (codeSessionWorkerEpochBody, bool) {
	body, err := readCodeSessionWorkerBody(w, r)
	if err != nil {
		writeCodeSessionWorkerBodyReadError(w, r, err)
		return codeSessionWorkerEpochBody{}, false
	}
	epoch, ok := s.validateWorkerEpochBody(w, r, codeSessionID, body)
	if !ok {
		return codeSessionWorkerEpochBody{}, false
	}
	return codeSessionWorkerEpochBody{body: body, epoch: epoch}, true
}

func (s *Service) validateWorkerEpochBody(w http.ResponseWriter, r *http.Request, codeSessionID string, body []byte) (int64, bool) {
	epoch, err := parseRequiredWorkerEpochFromBody(body)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return 0, false
	}
	if !s.validateWorkerEpochValue(w, r, codeSessionID, epoch) {
		return 0, false
	}
	return epoch, true
}

func (s *Service) validateOptionalWorkerEpochRequest(w http.ResponseWriter, r *http.Request, codeSessionID string) (int64, bool, bool) {
	epoch, found, err := parseOptionalWorkerEpochFromRequest(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return 0, false, false
	}
	if !found {
		return 0, false, true
	}
	if !s.validateWorkerEpochValue(w, r, codeSessionID, epoch) {
		return 0, true, false
	}
	return epoch, true, true
}

func (s *Service) validateWorkerEpochValue(w http.ResponseWriter, r *http.Request, codeSessionID string, epoch int64) bool {
	if err := s.db.ValidateCodeSessionWorkerEpoch(r.Context(), codeSessionID, epoch); err != nil {
		s.writeWorkerEpochDBError(w, r, codeSessionID, err, "Could not validate code session worker epoch")
		return false
	}
	return true
}

func (s *Service) writeWorkerEpochDBError(w http.ResponseWriter, r *http.Request, codeSessionID string, err error, internalMessage string) {
	if errors.Is(err, db.ErrNotFound) {
		writeIngressLoadError(w, r, err)
		return
	}
	if errors.Is(err, db.ErrWorkerEpochMismatch) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusConflict, "conflict_error", "Worker epoch mismatch"))
		return
	}
	log.Printf("code session worker epoch code_session_id=%s: %v", codeSessionID, err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", internalMessage))
}

func workerLeaseTimeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

func readCodeSessionWorkerBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBodySize)
	return io.ReadAll(r.Body)
}

func writeCodeSessionWorkerBodyReadError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "Request body is too large"))
		return
	}
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
}

func logCodeSessionWorkerInternalEventsBadRequest(r *http.Request, codeSessionID string, body []byte, err error) {
	bodyText, truncated := loggedWorkerRequestBody(body)
	query := ""
	path := ""
	if r.URL != nil {
		query = r.URL.RawQuery
		path = r.URL.Path
	}
	log.Printf("code session worker internal events bad request request_id=%s method=%s path=%s query=%q code_session_id=%s content_type=%q user_agent=%q content_length=%d body_bytes=%d body_truncated=%t error=%v body=%q",
		httpapi.RequestID(r.Context()),
		r.Method,
		path,
		query,
		codeSessionID,
		r.Header.Get("Content-Type"),
		r.Header.Get("User-Agent"),
		r.ContentLength,
		len(body),
		truncated,
		err,
		bodyText,
	)
}

func logCodeSessionWorkerOTLPRequest(r *http.Request, codeSessionID string, body []byte, epoch int64, epochFound bool, epochSource string, epochRawValue string, reason string, err error) {
	bodyText, bodyEncoding, truncated := loggedOTLPRequestBody(r, body)
	query := ""
	path := ""
	if r.URL != nil {
		query = r.URL.RawQuery
		path = r.URL.Path
	}
	epochValue := strings.TrimSpace(epochRawValue)
	if epochValue == "" && epochFound && epoch > 0 {
		epochValue = strconv.FormatInt(epoch, 10)
	}
	log.Printf("code session worker otlp request_id=%s signal=%s method=%s path=%s query=%q code_session_id=%s content_type=%q accept=%q user_agent=%q content_length=%d body_bytes=%d body_encoding=%s body_truncated=%t epoch_found=%t epoch_value=%q epoch_source=%q reason=%s error=%v body=%q",
		httpapi.RequestID(r.Context()),
		otlpSignalFromPath(path),
		r.Method,
		path,
		query,
		codeSessionID,
		r.Header.Get("Content-Type"),
		r.Header.Get("Accept"),
		r.Header.Get("User-Agent"),
		r.ContentLength,
		len(body),
		bodyEncoding,
		truncated,
		epochFound,
		epochValue,
		epochSource,
		reason,
		err,
		bodyText,
	)
}

func loggedWorkerRequestBody(body []byte) (string, bool) {
	if len(body) <= maxLoggedWorkerRequestBytes {
		return strings.ToValidUTF8(string(body), ""), false
	}
	return strings.ToValidUTF8(string(body[:maxLoggedWorkerRequestBytes]), ""), true
}

func loggedOTLPRequestBody(r *http.Request, body []byte) (string, string, bool) {
	truncated := len(body) > maxLoggedWorkerRequestBytes
	if truncated {
		body = body[:maxLoggedWorkerRequestBytes]
	}
	if otlpBodyLooksText(r) {
		return strings.ToValidUTF8(string(body), ""), "utf8", truncated
	}
	return base64.StdEncoding.EncodeToString(body), "base64", truncated
}

func otlpBodyLooksText(r *http.Request) bool {
	contentType := strings.TrimSpace(r.Header.Get("Content-Type"))
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	for _, exact := range []string{"application/json", "application/xml", "application/yaml", "application/x-yaml", "text/csv"} {
		if mediaType == exact {
			return true
		}
	}
	return false
}

func otlpSignalFromPath(path string) string {
	switch {
	case strings.HasSuffix(path, "/metrics"):
		return "metrics"
	case strings.HasSuffix(path, "/logs"):
		return "logs"
	default:
		// Only the metrics and logs worker OTLP HTTP endpoints are registered today.
		return ""
	}
}

func writeOTLPSuccess(w http.ResponseWriter, r *http.Request) {
	if otlpWantsJSONResponse(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func otlpWantsJSONResponse(r *http.Request) bool {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if strings.Contains(contentType, "json") {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(accept, "application/json")
}

func parseRequiredWorkerEpochFromBody(body []byte) (int64, error) {
	epoch, found, err := parseWorkerEpochFromJSONBody(body)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, errors.New("worker_epoch is required")
	}
	return epoch, nil
}

func parseWorkerEpochFromJSONBody(body []byte) (int64, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return 0, false, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, false, errors.New("Invalid JSON body")
	}
	raw, ok := envelope["worker_epoch"]
	if !ok {
		return 0, false, nil
	}
	epoch, err := parseWorkerEpochRaw(raw)
	if err != nil {
		return 0, true, err
	}
	return epoch, true, nil
}

func parseOptionalWorkerEpochFromRequest(r *http.Request) (int64, bool, error) {
	epoch, found, _, _, err := parseOptionalWorkerEpochFromRequestWithSource(r)
	return epoch, found, err
}

func parseOptionalWorkerEpochFromRequestWithSource(r *http.Request) (int64, bool, string, string, error) {
	value := ""
	source := ""
	if r.URL != nil {
		value = strings.TrimSpace(r.URL.Query().Get("worker_epoch"))
		if value != "" {
			source = "query:worker_epoch"
		}
	}
	if value == "" {
		for _, headerName := range []string{"x-worker-epoch", "worker-epoch", "worker_epoch"} {
			value = strings.TrimSpace(r.Header.Get(headerName))
			if value != "" {
				source = "header:" + headerName
				break
			}
		}
	}
	if value == "" {
		return 0, false, "", "", nil
	}
	epoch, err := parseWorkerEpochString(value)
	if err != nil {
		return 0, true, source, value, err
	}
	return epoch, true, source, value, nil
}

func parseCodeSessionWorkerStreamFromSequence(r *http.Request) (int64, error) {
	value := strings.TrimSpace(r.URL.Query().Get("from_sequence_num"))
	if value == "" {
		value = strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	}
	if value == "" {
		return 0, nil
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, errors.New("from_sequence_num must be a non-negative integer")
		}
	}
	sequence, err := strconv.ParseInt(value, 10, 64)
	if err != nil || sequence < 0 {
		return 0, errors.New("from_sequence_num must be a non-negative integer")
	}
	return sequence, nil
}

func parseWorkerEpochRaw(raw json.RawMessage) (int64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, errors.New("worker_epoch is required")
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, errors.New("worker_epoch must be an integer")
		}
		return parseWorkerEpochString(value)
	}
	return parseWorkerEpochString(string(raw))
}

func parseWorkerEpochString(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("worker_epoch is required")
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, errors.New("worker_epoch must be a positive integer")
		}
	}
	epoch, err := strconv.ParseInt(value, 10, 64)
	if err != nil || epoch <= 0 {
		return 0, errors.New("worker_epoch must be a positive integer")
	}
	return epoch, nil
}

func decodeIngressEventsBody(w http.ResponseWriter, r *http.Request) ([]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("events body is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil && envelope != nil {
		if rawEvents, ok := envelope["events"]; ok {
			var events []json.RawMessage
			if err := json.Unmarshal(rawEvents, &events); err != nil || len(events) == 0 {
				return nil, errors.New("events must be a non-empty array")
			}
			return events, nil
		}
		if _, ok := envelope["type"]; ok {
			return []json.RawMessage{json.RawMessage(body)}, nil
		}
	}
	var events []json.RawMessage
	if err := json.Unmarshal(body, &events); err == nil && len(events) > 0 {
		return events, nil
	}
	return nil, errors.New("events must be an array or object")
}

func validateCodeSessionWorkerRegisterBody(w http.ResponseWriter, r *http.Request, codeSessionID string) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBodySize)
	var envelope struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil && !errors.Is(err, io.EOF) {
		return errors.New("Invalid JSON body")
	}
	if sessionID := strings.TrimSpace(envelope.SessionID); sessionID != "" && sessionID != codeSessionID {
		return errors.New("session_id does not match code session id")
	}
	return nil
}

func decodeCodeSessionWorkerHeartbeatBody(w http.ResponseWriter, r *http.Request, codeSessionID string) (codeSessionWorkerHeartbeatInput, error) {
	body, err := readCodeSessionWorkerBody(w, r)
	if err != nil {
		return codeSessionWorkerHeartbeatInput{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return codeSessionWorkerHeartbeatInput{}, errors.New("heartbeat body is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return codeSessionWorkerHeartbeatInput{}, errors.New("heartbeat body must be a JSON object")
	}

	rawSessionID, ok := fields["session_id"]
	if !ok {
		return codeSessionWorkerHeartbeatInput{}, errors.New("session_id is required")
	}
	var sessionID string
	if err := json.Unmarshal(rawSessionID, &sessionID); err != nil {
		return codeSessionWorkerHeartbeatInput{}, errors.New("session_id must be a string")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return codeSessionWorkerHeartbeatInput{}, errors.New("session_id is required")
	}
	if sessionID != codeSessionID {
		return codeSessionWorkerHeartbeatInput{}, errors.New("session_id does not match code session id")
	}

	rawEpoch, ok := fields["worker_epoch"]
	if !ok {
		return codeSessionWorkerHeartbeatInput{}, errors.New("worker_epoch is required")
	}
	epoch, err := parseWorkerEpochRaw(rawEpoch)
	if err != nil {
		return codeSessionWorkerHeartbeatInput{}, err
	}
	return codeSessionWorkerHeartbeatInput{SessionID: sessionID, WorkerEpoch: epoch}, nil
}

func decodeCodeSessionWorkerStateBody(w http.ResponseWriter, r *http.Request) (db.UpdateCodeSessionWorkerStateInput, error) {
	body, err := readCodeSessionWorkerBody(w, r)
	if err != nil {
		return db.UpdateCodeSessionWorkerStateInput{}, err
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return db.UpdateCodeSessionWorkerStateInput{}, errors.New("worker body is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return db.UpdateCodeSessionWorkerStateInput{}, errors.New("worker body must be a JSON object")
	}

	rawEpoch, ok := fields["worker_epoch"]
	if !ok {
		return db.UpdateCodeSessionWorkerStateInput{}, errors.New("worker_epoch is required")
	}
	epoch, err := parseWorkerEpochRaw(rawEpoch)
	if err != nil {
		return db.UpdateCodeSessionWorkerStateInput{}, err
	}
	input := db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: epoch}

	if rawStatus, ok := fields["worker_status"]; ok {
		var status string
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			return db.UpdateCodeSessionWorkerStateInput{}, errors.New("worker_status must be a string")
		}
		status = strings.TrimSpace(status)
		if !validWorkerStatus(status) {
			return db.UpdateCodeSessionWorkerStateInput{}, errors.New("worker_status must be one of: idle, running, requires_action")
		}
		input.WorkerStatus = &status
	}

	if rawDetails, ok := fields["requires_action_details"]; ok {
		rawDetails = bytes.TrimSpace(rawDetails)
		if !rawJSONIsNull(rawDetails) && !rawJSONObject(rawDetails) {
			return db.UpdateCodeSessionWorkerStateInput{}, errors.New("requires_action_details must be an object or null")
		}
		input.RequiresActionDetailsSet = true
		if rawJSONIsNull(rawDetails) {
			input.RequiresActionDetails = json.RawMessage(`null`)
		} else {
			input.RequiresActionDetails = copyRawMessage(rawDetails)
		}
	}

	if rawMetadata, ok := fields["external_metadata"]; ok {
		rawMetadata = bytes.TrimSpace(rawMetadata)
		if !rawJSONObject(rawMetadata) {
			return db.UpdateCodeSessionWorkerStateInput{}, errors.New("external_metadata must be an object")
		}
		input.ExternalMetadataSet = true
		input.ExternalMetadata = copyRawMessage(rawMetadata)
	}

	return input, nil
}

func validWorkerStatus(status string) bool {
	switch status {
	case "idle", "running", "requires_action":
		return true
	default:
		return false
	}
}

func rawJSONIsNull(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || bytes.Equal(raw, []byte("null"))
}

func rawJSONObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func copyRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	copied := make([]byte, len(raw))
	copy(copied, raw)
	return json.RawMessage(copied)
}

func decodeCodeSessionWorkerEventsBody(w http.ResponseWriter, r *http.Request) ([]workerOutputEvent, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return decodeCodeSessionWorkerEventsPayload(body)
}

func decodeCodeSessionWorkerEventsPayload(body []byte) ([]workerOutputEvent, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("events body is required")
	}
	var envelope struct {
		Events []struct {
			Payload   json.RawMessage `json:"payload"`
			Ephemeral bool            `json:"ephemeral"`
		} `json:"events"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("Invalid JSON body")
	}
	if len(envelope.Events) == 0 {
		return nil, errors.New("events must be a non-empty array")
	}
	events := make([]workerOutputEvent, 0, len(envelope.Events))
	for i, event := range envelope.Events {
		payload := bytes.TrimSpace(event.Payload)
		if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
			return nil, fmt.Errorf("events[%d].payload is required", i)
		}
		object, err := decodeJSONObject(payload)
		if err != nil {
			return nil, fmt.Errorf("events[%d].payload must be a json object", i)
		}
		eventType := stringField(object, "type")
		if eventType == "" {
			return nil, fmt.Errorf("events[%d].payload.type is required", i)
		}
		if eventType != "keep_alive" && stringField(object, "uuid") == "" {
			return nil, fmt.Errorf("events[%d].payload.uuid is required", i)
		}
		events = append(events, workerOutputEvent{
			Payload:   json.RawMessage(append([]byte(nil), payload...)),
			Ephemeral: event.Ephemeral,
		})
	}
	return events, nil
}

func decodeCodeSessionWorkerDeliveryPayload(body []byte) ([]db.CodeSessionWorkerDeliveryUpdate, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("delivery body is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, errors.New("Invalid JSON body")
	}
	rawUpdates, ok := envelope["updates"]
	if !ok {
		return nil, errors.New("updates must be a non-empty array")
	}
	var items []struct {
		EventID string `json:"event_id"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(rawUpdates, &items); err != nil || items == nil {
		return nil, errors.New("updates must be a non-empty array")
	}
	if len(items) == 0 {
		return nil, errors.New("updates must be a non-empty array")
	}
	if len(items) > 64 {
		return nil, errors.New("updates must contain at most 64 items")
	}
	updates := make([]db.CodeSessionWorkerDeliveryUpdate, 0, len(items))
	for _, item := range items {
		eventID := strings.TrimSpace(item.EventID)
		if eventID == "" {
			return nil, errors.New("updates[].event_id is required")
		}
		status := strings.TrimSpace(item.Status)
		switch status {
		case "received", "processing", "processed":
		default:
			return nil, errors.New("updates[].status must be received, processing, or processed")
		}
		updates = append(updates, db.CodeSessionWorkerDeliveryUpdate{
			EventID: eventID,
			Status:  status,
		})
	}
	return updates, nil
}

func unwrapCodeSessionWorkerEvents(events []json.RawMessage) ([]json.RawMessage, error) {
	payloads := make([]json.RawMessage, 0, len(events))
	for _, event := range events {
		payload, err := unwrapCodeSessionWorkerEvent(event)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func unwrapCodeSessionWorkerEvent(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("event payload is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope != nil {
		if payload, ok := envelope["payload"]; ok {
			payload = bytes.TrimSpace(payload)
			if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
				return nil, errors.New("event payload is required")
			}
			return payload, nil
		}
	}
	return raw, nil
}

func decodeCodeSessionWorkerInternalEventsPayload(codeSessionID string, body []byte) ([]db.AppendCodeSessionInternalEventInput, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, errors.New("events body is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, errors.New("Invalid JSON body")
	}
	rawEvents, ok := envelope["events"]
	if !ok {
		return nil, errors.New("events is required")
	}
	if bytes.Equal(bytes.TrimSpace(rawEvents), []byte("null")) {
		return nil, errors.New("events must be an array")
	}
	var events []json.RawMessage
	if err := json.Unmarshal(rawEvents, &events); err != nil {
		return nil, errors.New("events must be an array")
	}
	inputs := make([]db.AppendCodeSessionInternalEventInput, 0, len(events))
	for _, raw := range events {
		input, err := decodeCodeSessionWorkerInternalEvent(codeSessionID, raw)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func decodeCodeSessionWorkerInternalEvent(codeSessionID string, raw json.RawMessage) (db.AppendCodeSessionInternalEventInput, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event is required")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope == nil {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event must be an object")
	}
	rawPayload, ok := envelope["payload"]
	if !ok {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event payload is required")
	}
	payload, payloadObject, err := normalizeJSONObject(rawPayload)
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event payload must be a json object")
	}
	if eventType := stringField(payloadObject, "type"); !isCodeSessionTranscriptPayloadType(eventType) {
		if eventType == "" {
			return db.AppendCodeSessionInternalEventInput{}, errors.New("event payload type is required")
		}
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event payload type must be user, assistant, attachment, or system")
	}
	payloadUUID := stringField(payloadObject, "uuid")
	if payloadUUID == "" {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event payload uuid is required")
	}
	isCompaction, err := optionalBoolRaw(envelope["is_compaction"], "is_compaction")
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	agentID, err := optionalStringRaw(envelope["agent_id"], "agent_id")
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	payloadAgentID := stringField(payloadObject, "agentId")
	if agentID != "" && payloadAgentID != "" && agentID != payloadAgentID {
		return db.AppendCodeSessionInternalEventInput{}, errors.New("event agent_id and payload.agentId must match")
	}
	if agentID == "" {
		agentID = payloadAgentID
	}
	var agentIDPtr *string
	if agentID != "" {
		agentIDPtr = &agentID
	}
	meta, err := BuildEventMetadata(codeSessionID, "internal", payload)
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	eventMetadata, err := optionalJSONRaw(envelope["event_metadata"], "event_metadata")
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	eventID, err := ids.New("csie_")
	if err != nil {
		return db.AppendCodeSessionInternalEventInput{}, err
	}
	return db.AppendCodeSessionInternalEventInput{
		ExternalID:     eventID,
		EventType:      meta.EventType,
		PayloadUUID:    payloadUUID,
		AgentID:        agentIDPtr,
		IsCompaction:   isCompaction,
		Payload:        meta.Payload,
		PayloadHash:    meta.PayloadHash,
		IdempotencyKey: meta.IdempotencyKey,
		EventMetadata:  eventMetadata,
	}, nil
}

func isCodeSessionTranscriptPayloadType(eventType string) bool {
	switch eventType {
	case "user", "assistant", "attachment", "system":
		return true
	default:
		return false
	}
}

func optionalStringRaw(raw json.RawMessage, field string) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New(field + " must be a string")
	}
	return strings.TrimSpace(value), nil
}

func optionalBoolRaw(raw json.RawMessage, field string) (bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, errors.New(field + " must be a boolean")
	}
	return value, nil
}

func optionalJSONRaw(raw json.RawMessage, field string) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New(field + " must be valid json")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New(field + " must contain a single json value")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func parseCodeSessionInternalEventsCursor(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, errors.New("cursor must be a non-negative integer")
		}
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0, errors.New("cursor must be a non-negative integer")
	}
	return cursor, nil
}

func codeSessionInternalEventResponse(event db.CodeSessionInternalEvent) map[string]any {
	response := map[string]any{
		"event_id":       event.ExternalID,
		"event_type":     event.EventType,
		"payload":        event.Payload,
		"event_metadata": nil,
		"is_compaction":  event.IsCompaction,
		"created_at":     formatTime(event.CreatedAt),
	}
	if len(bytes.TrimSpace(event.EventMetadata)) > 0 && !bytes.Equal(bytes.TrimSpace(event.EventMetadata), []byte("null")) {
		response["event_metadata"] = event.EventMetadata
	}
	if event.AgentID != nil && strings.TrimSpace(*event.AgentID) != "" {
		response["agent_id"] = strings.TrimSpace(*event.AgentID)
	}
	return response
}

func decodeDiagLogBody(w http.ResponseWriter, r *http.Request) ([]json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return decodeDiagLogPayload(body)
}

func decodeDiagLogPayload(body []byte) ([]json.RawMessage, error) {
	var envelope struct {
		Lines []any `json:"lines"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(body), &envelope); err != nil {
		return nil, errors.New("Invalid JSON body")
	}
	if len(envelope.Lines) == 0 {
		return nil, errors.New("lines must be a non-empty array")
	}
	payloads := make([]json.RawMessage, 0, len(envelope.Lines))
	for _, line := range envelope.Lines {
		payload, err := diagLogEvent(line)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func diagLogEvent(line any) (json.RawMessage, error) {
	if object, ok := line.(map[string]any); ok {
		if strings.TrimSpace(stringField(object, "type")) == "env_manager_log" {
			if stringField(object, "uuid") == "" {
				object["uuid"] = uuid.NewString()
			}
			return marshalRaw(object)
		}
	}
	return marshalRaw(map[string]any{
		"type": "env_manager_log",
		"uuid": uuid.NewString(),
		"data": line,
	})
}
