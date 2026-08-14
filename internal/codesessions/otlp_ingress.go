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

// handleCodeSessionWorkerOTLP 是 worker 遥测上报入口：
// 鉴权 → 协议/信号 → 并发准入 → epoch/lease 栅栏 → 读 body → 注入可信属性 → 转发。
// 所有拒绝判定都发生在读 body 之前。
func (h *Handler) handleCodeSessionWorkerOTLP(w http.ResponseWriter, r *http.Request) {
	codeSessionID := chi.URLParam(r, "code_session_id")
	protocol, protocolError := parseOTLPProtocol(r.Header.Get("Content-Type"))
	// 鉴权：先于协议错误，协议未知时错误体退回 JSON。
	claims, authError := h.sessionIngressClaims(r, codeSessionID)
	if authError != nil {
		if protocolError != nil {
			protocol = otlpProtocolJSON
		}
		status, message := otlpSessionIngressAuthStatus(authError)
		writeOTLPStatus(w, protocol, status, message, "")
		return
	}

	if protocolError != nil {
		writeOTLPStatus(w, otlpProtocolJSON, http.StatusUnsupportedMediaType, protocolError.Error(), "")
		return
	}
	signal, err := otlpEndpointFromPath(r.URL.Path)
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusNotFound, err.Error(), "")
		return
	}
	if h.otlpSink == nil || !h.cfg.Observability.Enabled {
		writeOTLPStatus(w, protocol, http.StatusServiceUnavailable, "agent observability is not enabled", "")
		return
	}

	release, admitted := h.otlpAdmission.acquire(codeSessionID)
	if !admitted {
		writeOTLPStatus(w, protocol, http.StatusTooManyRequests, "OTLP ingress is busy", "1")
		return
	}
	defer release()

	workerEpoch, err := parseOTLPWorkerEpoch(r)
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusBadRequest, err.Error(), "")
		return
	}

	// 可信租户属性的唯一来源；注入 payload 的身份全部取自它而非 worker 输入。
	credentialContext, err := h.db.GetCodeSessionCredentialContextForIssue(
		r.Context(),
		claims.OrganizationUUID,
		claims.WorkspaceUUID,
		codeSessionID,
	)
	if err != nil {
		h.writeOTLPDatabaseError(w, r, protocol, codeSessionID, err)
		return
	}
	if err := h.db.ValidateCodeSessionWorkerActiveLease(r.Context(), codeSessionID, workerEpoch); err != nil {
		h.writeOTLPDatabaseError(w, r, protocol, codeSessionID, err)
		return
	}
	// session 重建或换绑后，旧 token 即使未过期也必须被拒。
	if !credentialContextMatchesClaims(credentialContext, claims) {
		writeOTLPStatus(w, protocol, http.StatusUnauthorized, "session ingress identity no longer matches the active code session", "")
		return
	}

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
	canonicalBody, err := canonicalizeOTLPPayload(signal, protocol, body, trustedOTLPResourceAttributes{
		organizationUUID: credentialContext.OrganizationUUID,
		workspaceUUID:    credentialContext.WorkspaceUUID,
		publicSessionID:  credentialContext.PublicSessionExternalID,
		codeSessionID:    credentialContext.CodeSessionExternalID,
		agentID:          credentialContext.AgentExternalID,
		agentVersion:     int64(credentialContext.AgentVersion),
		workerEpoch:      workerEpoch,
	})
	if err != nil {
		writeOTLPStatus(w, protocol, http.StatusBadRequest, err.Error(), "")
		return
	}

	response, err := h.otlpSink.forward(r.Context(), signal, protocol, canonicalBody)
	if err != nil {
		status := http.StatusServiceUnavailable
		retryAfter := ""
		var sinkError *otlpSinkError
		if errors.As(err, &sinkError) {
			status = sinkError.statusCode
			retryAfter = sinkError.retryAfter
		}
		// 上游细节只进日志，对 worker 只回固定文案。
		h.logger.WarnContext(r.Context(), "forward code session OTLP", "code_session_id", codeSessionID, "signal", signal, "status", status, "error", err)
		writeOTLPStatus(w, protocol, status, "OTLP backend rejected the request", retryAfter)
		return
	}

	writeOTLPSuccess(w, protocol, response.body)
}

func parseOTLPWorkerEpoch(r *http.Request) (int64, error) {
	value := strings.TrimSpace(r.Header.Get("x-worker-epoch"))
	if value == "" {
		return 0, errors.New("x-worker-epoch header is required")
	}
	return parseWorkerEpochString(value)
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
	case errors.Is(err, db.ErrNotFound):
		writeOTLPStatus(w, protocol, http.StatusGone, "code session is no longer active", "")
	case errors.Is(err, db.ErrWorkerEpochMismatch):
		writeOTLPStatus(w, protocol, http.StatusConflict, "worker epoch mismatch", "")
	case errors.Is(err, db.ErrWorkerLeaseExpired):
		writeOTLPStatus(w, protocol, http.StatusGone, "code session worker lease expired", "")
	default:
		h.logger.ErrorContext(r.Context(), "validate code session OTLP lifecycle", "code_session_id", codeSessionID, "error", err)
		writeOTLPStatus(w, protocol, http.StatusServiceUnavailable, "could not validate code session lifecycle", "")
	}
}

func otlpEndpointFromPath(path string) (string, error) {
	path = strings.TrimRight(path, "/")
	switch {
	case strings.HasSuffix(path, "/otlp/metrics"):
		return "metrics", nil
	case strings.HasSuffix(path, "/otlp/logs"), strings.HasSuffix(path, "/otlp/v1/logs"):
		return "logs", nil
	case strings.HasSuffix(path, "/otlp/v1/traces"):
		return "traces", nil
	default:
		return "", errors.New("unsupported OTLP endpoint")
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
