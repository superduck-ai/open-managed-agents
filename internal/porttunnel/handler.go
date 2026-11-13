package porttunnel

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const (
	Path                   = "/v2/sandbox/tunnel"
	defaultRevalidateEvery = 30 * time.Second
	revalidateJitter       = 0.2
	codeSessionIDHeader    = "X-Session-ID"
)

type Authenticator interface {
	AuthenticateTunnel(ctx context.Context, token, codeSessionExternalID string) (Identity, error)
}

type Handler struct {
	auth            Authenticator
	registry        *Registry
	limits          Limits
	logger          *slog.Logger
	upgrader        websocket.Upgrader
	revalidateEvery time.Duration
	readIdleTimeout time.Duration
	stallLimit      time.Duration
}

func NewHandler(authenticator Authenticator, registry *Registry, limits Limits, logger *slog.Logger) *Handler {
	return &Handler{
		auth:     authenticator,
		registry: registry,
		limits:   limits,
		logger:   logging.LoggerOrDefault(logger),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
		revalidateEvery: defaultRevalidateEvery,
		readIdleTimeout: defaultReadIdleTimeout,
		stallLimit:      StallTimeout,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := auth.ExtractBearerToken(r)
	codeSessionID := strings.TrimSpace(r.Header.Get(codeSessionIDHeader))
	if token == "" || codeSessionID == "" {
		httpapi.WriteError(w, r, tunnelCredentialMissing())
		return
	}
	identity, err := h.auth.AuthenticateTunnel(r.Context(), token, codeSessionID)
	if err != nil {
		h.writeAuthFailure(w, r, err)
		return
	}
	flowControl := requestsFlowControl(r.Header)
	var responseHeader http.Header
	if flowControl {
		responseHeader = http.Header{CapabilitiesHeader: []string{FlowControlCapability}}
	}
	socket, err := h.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		h.logger.WarnContext(r.Context(), "upgrade session port tunnel", "code_session_id", codeSessionID, "error", err)
		return
	}
	conn := newConn(identity, socket, h.limits, h.logger)
	conn.readIdleTimeout = h.readIdleTimeout
	conn.stallLimit = h.stallLimit
	conn.flowControl = flowControl
	h.registry.register(conn)
	defer h.registry.unregister(conn)
	go h.revalidate(conn, token)
	h.logger.InfoContext(r.Context(), "session port tunnel connected", "code_session_id", codeSessionID, "flow_control", flowControl)
	runErr := conn.run()
	h.logger.InfoContext(r.Context(), "session port tunnel disconnected", "code_session_id", codeSessionID, "reason", closeReason(conn.Err(), runErr))
}

func (h *Handler) writeAuthFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnauthorized) {
		httpapi.WriteError(w, r, tunnelCredentialInvalid())
		return
	}
	httpapi.LogFailure(h.logger, r, "authenticate session port tunnel", "error", err)
	httpapi.WriteError(w, r, tunnelAuthenticationUnavailable())
}

func (h *Handler) revalidate(conn *Conn, token string) {
	timer := time.NewTimer(jittered(h.revalidateEvery))
	defer timer.Stop()
	for {
		select {
		case <-conn.Done():
			return
		case <-timer.C:
			timer.Reset(jittered(h.revalidateEvery))
		}
		ctx, cancel := context.WithTimeout(context.Background(), h.revalidateEvery)
		_, err := h.auth.AuthenticateTunnel(ctx, token, conn.identity.CodeSessionExternalID)
		cancel()
		if errors.Is(err, ErrUnauthorized) {
			conn.Close(err)
			return
		}
		if err != nil {
			h.logger.Warn("revalidate session port tunnel", "code_session_id", conn.identity.CodeSessionExternalID, "error", err)
		}
	}
}

func jittered(interval time.Duration) time.Duration {
	spread := float64(interval) * revalidateJitter
	return interval + time.Duration((rand.Float64()*2-1)*spread)
}

func closeReason(closeErr, runErr error) string {
	switch {
	case errors.Is(closeErr, errReplaced):
		return "replaced"
	case errors.Is(closeErr, ErrUnauthorized):
		return "credential_revoked"
	case websocket.IsCloseError(runErr, websocket.CloseNormalClosure, websocket.CloseGoingAway):
		return "client_closed"
	default:
		return "connection_lost"
	}
}
