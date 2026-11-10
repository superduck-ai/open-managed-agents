package channels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type agentStore interface {
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetEnvironment(context.Context, string, string) (db.Environment, error)
	EnsureChannelWorkspaceCredential(context.Context, platform.CreateConsoleAPIKeyInput, *secrets.Service) (string, error)
	ListAgentsPage(context.Context, db.ListAgentsPageParams) ([]db.Agent, bool, error)
}

type handler struct {
	store  agentStore
	cfg    config.ChannelBridgeConfig
	client *http.Client
	cipher *secrets.Service
	logger *slog.Logger
}

func newChannelHandler(cfg config.ChannelBridgeConfig, store agentStore, logger *slog.Logger, cipher *secrets.Service) *handler {
	return &handler{
		store:  store,
		cfg:    cfg,
		cipher: cipher,
		logger: logging.LoggerOrDefault(logger),
		client: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func NewHandler(cfg config.ChannelBridgeConfig, store agentStore, logger *slog.Logger, cipher *secrets.Service) http.Handler {
	return newChannelHandler(cfg, store, logger, cipher).agentRouter()
}

func NewWorkspaceHandler(cfg config.ChannelBridgeConfig, store agentStore, logger *slog.Logger, cipher *secrets.Service) http.Handler {
	return newChannelHandler(cfg, store, logger, cipher).workspaceRouter()
}

func (h *handler) agentRouter() http.Handler {
	r := chi.NewRouter()
	wrap := httpapi.NewErrorAdapter(h.logger).Wrap
	r.Get("/", wrap(h.forward))
	r.Post("/", wrap(h.forward))
	r.Get("/settings", wrap(h.forward))
	r.Post("/settings", wrap(h.forward))
	r.Post("/{channel_id}", wrap(h.forward))
	r.Delete("/{channel_id}", wrap(h.forward))
	r.Post("/{channel_id}/qr", wrap(h.forward))
	r.Get("/{channel_id}/qr", wrap(h.forward))
	r.Get("/{channel_id}/pairings", wrap(h.forward))
	r.Post("/{channel_id}/pairings/{pairing_id}", wrap(h.forward))
	return r
}

func (h *handler) forward(w http.ResponseWriter, r *http.Request) error {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.WorkspaceUUID == "" || p.OrganizationUUID == "" {
		return unauthenticated()
	}
	if err := rejectBrowserCSRF(r, p); err != nil {
		return err
	}
	agentID := chi.URLParam(r, "agent_id")
	agent, err := h.store.GetAgent(r.Context(), p.WorkspaceUUID, agentID)
	if errors.Is(err, db.ErrNotFound) {
		return agentNotFound()
	}
	if err != nil {
		return unavailable(err)
	}
	if agent.ArchivedAt != nil && r.Method != http.MethodGet {
		return archivedAgent()
	}
	settings := strings.HasSuffix(chi.RouteContext(r.Context()).RoutePattern(), "/settings")
	if settings && r.Method == http.MethodPost {
		if err := h.validateEnvironment(w, r, p.WorkspaceUUID); err != nil {
			return err
		}
	}
	runtimeRequired, err := requiresRuntime(w, r, settings)
	if err != nil {
		return err
	}
	endpoint, err := h.endpoint(r, settings)
	if err != nil {
		return err
	}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, endpoint.String(), http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		return unavailable(err)
	}
	upstream.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("X-OMA-Organization", p.OrganizationUUID)
	upstream.Header.Set("X-OMA-Workspace", p.WorkspaceUUID)
	upstream.Header.Set("X-OMA-Agent", agentID)
	if p.CredentialType == auth.CredentialTypeAPIKey {
		upstream.Header.Set("X-OMA-Source", "api")
	}
	if runtimeRequired {
		if err := h.requireEnvironment(upstream, p.WorkspaceUUID); err != nil {
			return err
		}
		if err := h.syncWorkspaceCredentials(upstream, p); err != nil {
			return err
		}
	}
	response, err := h.client.Do(upstream)
	if err != nil {
		return unavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 || response.StatusCode == 401 || response.StatusCode == 403 {
		return unavailable(fmt.Errorf("桥接请求返回 HTTP %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return unavailable(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, err = w.Write(body)
	return err
}

func rejectBrowserCSRF(r *http.Request, principal auth.Principal) error {
	if principal.CredentialType != auth.CredentialTypePlatformSession {
		return nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	default:
		sessionKey := auth.ExtractPlatformSessionKey(r)
		if sessionKey == "" || !auth.ValidatePlatformCSRFToken(sessionKey, r.Header.Get("X-CSRF-Token")) {
			return csrfRejected()
		}
		return nil
	}
}

func (h *handler) endpoint(r *http.Request, settings bool) (*url.URL, error) {
	endpoint, err := h.baseEndpoint()
	if err != nil {
		return nil, err
	}

	if settings {
		endpoint.Path += "/settings"
	} else if channelID := chi.URLParam(r, "channel_id"); channelID != "" {
		endpoint.Path += "/" + url.PathEscape(channelID)
		if strings.HasSuffix(chi.RouteContext(r.Context()).RoutePattern(), "/qr") {
			endpoint.Path += "/qr"
		}
		if strings.Contains(chi.RouteContext(r.Context()).RoutePattern(), "/pairings") {
			endpoint.Path += "/pairings"
		}
		if pairingID := chi.URLParam(r, "pairing_id"); pairingID != "" {
			endpoint.Path += "/" + url.PathEscape(pairingID)
		}
	}
	return endpoint, nil
}

func (h *handler) baseEndpoint() (*url.URL, error) {
	endpoint, err := url.Parse(h.cfg.URL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || h.cfg.Token == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, unavailable(err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/managed-channels"
	endpoint.RawQuery = ""
	return endpoint, nil
}
