package codesessions

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

const (
	defaultOTLPGlobalConcurrency  = 32
	defaultOTLPSessionConcurrency = 2
)

// otlpAdmission bounds OTLP work before decoding or forwarding. The per-session
// limit prevents one noisy worker from consuming the whole process budget.
type otlpAdmission struct {
	global chan struct{}
	limit  int

	mu       sync.Mutex
	sessions map[string]int
}

func newOTLPAdmission(globalLimit, sessionLimit int) *otlpAdmission {
	if globalLimit <= 0 {
		globalLimit = 1
	}
	if sessionLimit <= 0 {
		sessionLimit = 1
	}
	return &otlpAdmission{
		global:   make(chan struct{}, globalLimit),
		limit:    sessionLimit,
		sessions: make(map[string]int),
	}
}

func (a *otlpAdmission) acquire(sessionID string) (func(), bool) {
	select {
	case a.global <- struct{}{}:
	default:
		return nil, false
	}

	a.mu.Lock()
	if a.sessions[sessionID] >= a.limit {
		a.mu.Unlock()
		<-a.global
		return nil, false
	}
	a.sessions[sessionID]++
	a.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			a.sessions[sessionID]--
			if a.sessions[sessionID] == 0 {
				delete(a.sessions, sessionID)
			}
			a.mu.Unlock()
			<-a.global
		})
	}, true
}

type otlpWorkerRequest struct {
	codeSessionID     string
	protocol          otlpProtocol
	credentialContext db.CodeSessionCredentialContext
	release           func()
}

func (h *Handler) admitWorkerOTLP(w http.ResponseWriter, r *http.Request, admission *otlpAdmission, available bool, unavailable string) (otlpWorkerRequest, bool) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	protocol, protocolError := parseOTLPProtocol(r.Header.Get("Content-Type"))
	claims, authError := h.sessionIngressClaims(r, codeSessionID)
	if authError != nil {
		if protocolError != nil {
			protocol = otlpProtocolJSON
		}
		status, message := otlpSessionIngressAuthStatus(authError)
		writeOTLPStatus(w, protocol, status, message, "")
		return otlpWorkerRequest{}, false
	}
	if protocolError != nil {
		writeOTLPStatus(w, otlpProtocolJSON, http.StatusUnsupportedMediaType, protocolError.Error(), "")
		return otlpWorkerRequest{}, false
	}
	if err := h.service.authorizeWorkerClaims(r.Context(), claims); err != nil {
		h.writeOTLPDatabaseError(w, r, protocol, codeSessionID, err)
		return otlpWorkerRequest{}, false
	}
	if !available {
		writeOTLPStatus(w, protocol, http.StatusServiceUnavailable, unavailable, "")
		return otlpWorkerRequest{}, false
	}
	release, admitted := admission.acquire(codeSessionID)
	if !admitted {
		writeOTLPStatus(w, protocol, http.StatusTooManyRequests, "OTLP ingress is busy", "1")
		return otlpWorkerRequest{}, false
	}
	credentialContext, err := h.db.GetCodeSessionCredentialContextForIssue(r.Context(), claims.OrganizationUUID, claims.WorkspaceUUID, codeSessionID)
	if err == nil {
		err = h.db.ValidateCodeSessionWorkerActiveLease(r.Context(), codeSessionID)
	}
	if err != nil {
		release()
		h.writeOTLPDatabaseError(w, r, protocol, codeSessionID, err)
		return otlpWorkerRequest{}, false
	}
	// session 重建或换绑后，旧 token 即使未过期也必须被拒。
	if !credentialContextMatchesClaims(credentialContext, claims) {
		release()
		writeOTLPStatus(w, protocol, http.StatusUnauthorized, "session ingress identity no longer matches the active code session", "")
		return otlpWorkerRequest{}, false
	}
	return otlpWorkerRequest{codeSessionID: codeSessionID, protocol: protocol, credentialContext: credentialContext, release: release}, true
}

func (h *Handler) handleCodeSessionWorkerOTLP(w http.ResponseWriter, r *http.Request) {
	admitted, ok := h.admitWorkerOTLP(w, r, h.otlpAdmission, h.otlpSink != nil && h.cfg.Observability.Enabled, "agent observability is not enabled")
	if !ok {
		return
	}
	defer admitted.release()
	codeSessionID, protocol, credentialContext := admitted.codeSessionID, admitted.protocol, admitted.credentialContext

	body, err := readOTLPRequestBody(w, r, h.cfg.Observability.OTLP.MaxRequestBytes)
	if err != nil {
		status := http.StatusBadRequest
		var bodyError *otlpBodyError
		if errors.As(err, &bodyError) {
			status = bodyError.statusCode
		}
		writeOTLPStatus(w, protocol, status, err.Error(), "")
		return
	}
	canonicalBody, err := prepareNativeOTLP(protocol, body, trustedOTLPResourceAttributes{
		organizationUUID: credentialContext.OrganizationUUID,
		workspaceUUID:    credentialContext.WorkspaceUUID,
		publicSessionID:  credentialContext.PublicSessionExternalID,
		codeSessionID:    credentialContext.CodeSessionExternalID,
		agentID:          credentialContext.AgentExternalID,
		agentVersion:     int64(credentialContext.AgentVersion),
	}, h.cfg.Observability.ContentCaptureEnabled)
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusBadRequest, err.Error(), "")
		return
	}

	response, err := h.otlpSink.forward(r.Context(), protocol, canonicalBody)
	if err != nil {
		status := http.StatusServiceUnavailable
		retryAfter := ""
		var sinkError *otlpSinkError
		if errors.As(err, &sinkError) {
			status = sinkError.statusCode
			retryAfter = sinkError.retryAfter
		}
		// 上游细节只进日志，对 worker 只回固定文案。
		h.logger.WarnContext(r.Context(), "forward code session OTLP", "code_session_id", codeSessionID, "status", status, "error", err)
		writeOTLPStatus(w, protocol, status, "OTLP backend rejected the request", retryAfter)
		return
	}

	writeOTLPSuccess(w, protocol, response.body)
}

// otlpSessionIngressAuthStatus 把 sessionIngressClaims 的 apperr 翻译成 OTLP
// 响应的 HTTP 状态与公开文案（NotFound → 404，其余均为鉴权失败 → 401）。
func otlpSessionIngressAuthStatus(err error) (int, string) {
	var appError *apperr.Error
	if !errors.As(err, &appError) {
		return http.StatusUnauthorized, "Unauthorized"
	}
	if appError.Kind == apperr.NotFound {
		return http.StatusNotFound, appError.PublicMessage
	}
	return http.StatusUnauthorized, appError.PublicMessage
}

func credentialContextMatchesClaims(context db.CodeSessionCredentialContext, claims SessionCredentialClaims) bool {
	return context.CodeSessionExternalID == claims.SessionID &&
		context.PublicSessionExternalID == claims.PublicSessionID &&
		context.AgentExternalID == claims.AgentID &&
		context.AgentVersion == claims.AgentVersion &&
		context.OrganizationUUID == claims.OrganizationUUID &&
		context.WorkspaceUUID == claims.WorkspaceUUID
}

func (h *Handler) writeOTLPDatabaseError(w http.ResponseWriter, r *http.Request, protocol otlpProtocol, codeSessionID string, err error) {
	switch {
	case errors.Is(err, errSessionIngressIdentity):
		writeOTLPStatus(w, protocol, http.StatusUnauthorized, "session ingress identity is no longer active", "")
	case errors.Is(err, db.ErrWorkerEpochMismatch):
		writeOTLPStatus(w, protocol, http.StatusConflict, "Worker epoch mismatch", "")
	case errors.Is(err, db.ErrNotFound):
		writeOTLPStatus(w, protocol, http.StatusGone, "code session is no longer active", "")
	case errors.Is(err, db.ErrWorkerLeaseExpired):
		writeOTLPStatus(w, protocol, http.StatusGone, "code session worker lease expired", "")
	default:
		h.logger.ErrorContext(r.Context(), "validate code session OTLP lifecycle", "code_session_id", codeSessionID, "error", err)
		writeOTLPStatus(w, protocol, http.StatusServiceUnavailable, "could not validate code session lifecycle", "")
	}
}

type otlpBodyError struct {
	statusCode int
	message    string
}

func (e *otlpBodyError) Error() string { return e.message }

func readOTLPRequestBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, &otlpBodyError{statusCode: http.StatusServiceUnavailable, message: "OTLP ingress is not configured"}
	}
	if r.Body == nil {
		return nil, &otlpBodyError{statusCode: http.StatusBadRequest, message: "OTLP request body is required"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	encoded, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return nil, &otlpBodyError{statusCode: http.StatusRequestEntityTooLarge, message: "OTLP request body is too large"}
		}
		return nil, &otlpBodyError{statusCode: http.StatusBadRequest, message: "could not read OTLP request body"}
	}

	var reader io.Reader = bytes.NewReader(encoded)
	switch encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); encoding {
	case "", "identity":
	case "gzip":
		gzipReader, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			return nil, &otlpBodyError{statusCode: http.StatusBadRequest, message: "invalid gzip OTLP request body"}
		}
		defer gzipReader.Close()
		reader = gzipReader
	default:
		return nil, &otlpBodyError{statusCode: http.StatusUnsupportedMediaType, message: "unsupported OTLP Content-Encoding"}
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, &otlpBodyError{statusCode: http.StatusBadRequest, message: "could not read OTLP request body"}
	}
	if int64(len(body)) > maxBytes {
		return nil, &otlpBodyError{statusCode: http.StatusRequestEntityTooLarge, message: "OTLP request body is too large"}
	}
	return body, nil
}
