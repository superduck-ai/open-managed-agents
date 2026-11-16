package portgateway

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
	"golang.org/x/sync/singleflight"
)

const (
	portAccessInterval  = time.Minute
	backgroundOpTimeout = 10 * time.Second
)

type Store interface {
	FindSessionPortTarget(ctx context.Context, sessionUUID string) (db.SessionPortTarget, error)
	TouchCodeSessionPortAccess(ctx context.Context, organizationUUID, workspaceUUID, codeSessionUUID string, now time.Time, interval time.Duration) error
}

type SandboxTimeoutExtender interface {
	SetTimeout(ctx context.Context, sandboxID string, timeout time.Duration) error
}

type Deps struct {
	Settings       *sessionports.Settings
	Config         config.SessionPortsConfig
	SandboxTimeout time.Duration
	Store          Store
	Router         porttunnel.Router
	Sandboxes      SandboxTimeoutExtender
	Logger         *slog.Logger

	CallerWriteTimeout            time.Duration
	FlowControlCallerWriteTimeout time.Duration
}

type Gateway struct {
	settings       *sessionports.Settings
	cfg            config.SessionPortsConfig
	sandboxTimeout time.Duration
	store          Store
	router         porttunnel.Router
	sandboxes      SandboxTimeoutExtender
	logger         *slog.Logger
	writeTimeout   time.Duration
	flowTimeout    time.Duration
	wakes          singleflight.Group
	renewals       *throttle
	touches        *throttle
	upgrader       websocket.Upgrader
}

type target struct {
	address sessionports.Address
	session db.SessionPortTarget
}

func New(deps Deps) *Gateway {
	return &Gateway{
		settings:       deps.Settings,
		cfg:            deps.Config,
		sandboxTimeout: deps.SandboxTimeout,
		store:          deps.Store,
		router:         deps.Router,
		sandboxes:      deps.Sandboxes,
		logger:         logging.LoggerOrDefault(deps.Logger),
		writeTimeout:   cmp.Or(deps.CallerWriteTimeout, porttunnel.StallTimeout),
		flowTimeout:    cmp.Or(deps.FlowControlCallerWriteTimeout, porttunnel.FlowControlledWriteTimeout),
		renewals:       newThrottle(deps.SandboxTimeout / 3),
		touches:        newThrottle(portAccessInterval),
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
	}
}

func (g *Gateway) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.settings.MatchesHost(r.Host) {
			g.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resolved, failure := g.resolve(r.Context(), r.Host)
	if failure != nil {
		g.fail(w, r, failure)
		return
	}
	tunnel, failure := g.connect(r.Context(), resolved)
	if failure != nil {
		g.fail(w, r, failure)
		return
	}
	g.keepAlive(resolved)
	if websocket.IsWebSocketUpgrade(r) {
		g.proxyWebSocket(w, r, resolved, tunnel)
		return
	}
	g.proxyHTTP(w, r, resolved, tunnel)
}

func (g *Gateway) fail(w http.ResponseWriter, r *http.Request, failure *gatewayError) {
	if failure.status >= http.StatusInternalServerError && failure.status != http.StatusServiceUnavailable {
		g.logger.WarnContext(r.Context(), "session port request failed", "status", failure.status, "code", failure.code, "error", failure)
	}
	writeGatewayError(w, failure)
}

func (g *Gateway) resolve(ctx context.Context, host string) (target, *gatewayError) {
	address, err := g.settings.ParseHost(host)
	if err != nil {
		return target{}, notFound(err)
	}
	session, err := g.store.FindSessionPortTarget(ctx, address.SessionUUID)
	if errors.Is(err, db.ErrNotFound) {
		return target{}, notFound(err)
	}
	if err != nil {
		return target{}, internalFailure(err)
	}
	if session.Removed {
		return target{}, notFound(nil)
	}
	if session.CodeSessionUUID == "" || session.ProviderSandboxID == "" ||
		(session.SandboxState != sandboxRunning && session.SandboxState != sandboxCreating) {
		return target{}, sandboxUnavailable(nil)
	}
	return target{address: address, session: session}, nil
}

func (g *Gateway) keepAlive(resolved target) {
	now := time.Now()
	codeSessionUUID := resolved.session.CodeSessionUUID
	if g.touches.allow(codeSessionUUID, now) {
		go g.touchPortAccess(resolved.session, now)
	}
	g.renewIfDue(resolved.session, now)
}

func (g *Gateway) touchPortAccess(session db.SessionPortTarget, now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), backgroundOpTimeout)
	defer cancel()
	if err := g.store.TouchCodeSessionPortAccess(ctx, session.OrganizationUUID, session.WorkspaceUUID, session.CodeSessionUUID, now, portAccessInterval); err != nil {
		g.touches.forget(session.CodeSessionUUID)
		g.logger.WarnContext(ctx, "record session port access", "session_id", session.SessionExternalID, "error", err)
	}
}

func (g *Gateway) renewIfDue(session db.SessionPortTarget, now time.Time) {
	if session.SandboxState != sandboxRunning || !g.renewals.allow(session.ProviderSandboxID, now) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backgroundOpTimeout)
		defer cancel()
		if err := g.sandboxes.SetTimeout(ctx, session.ProviderSandboxID, g.sandboxTimeout); err != nil {
			g.renewals.forget(session.ProviderSandboxID)
			g.logger.WarnContext(ctx, "renew session port sandbox", "session_id", session.SessionExternalID, "error", err)
		}
	}()
}

func (g *Gateway) callerWriteTimeout(tunnel porttunnel.Tunnel) time.Duration {
	if tunnel.FlowControl() {
		return g.flowTimeout
	}
	return g.writeTimeout
}
