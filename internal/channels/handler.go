package channels

import (
	"context"
	"errors"
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
)

type agentStore interface {
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetEnvironment(context.Context, string, string) (db.Environment, error)
}

type handler struct {
	store  agentStore
	cfg    config.ChannelBridgeConfig
	client *http.Client
}

func NewHandler(cfg config.ChannelBridgeConfig, store agentStore, logger *slog.Logger) http.Handler {
	h := &handler{store: store, cfg: cfg, client: &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	r := chi.NewRouter()
	wrap := httpapi.NewErrorAdapter(logger).Wrap
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
	if p.CredentialType == auth.CredentialTypePlatformSession {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			sessionKey := auth.ExtractPlatformSessionKey(r)
			if sessionKey == "" || !auth.ValidatePlatformCSRFToken(sessionKey, r.Header.Get("X-CSRF-Token")) {
				return csrfRejected()
			}
		}
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
	settings := strings.HasSuffix(r.URL.Path, "/settings")
	if settings && r.Method == http.MethodPost {
		if err := h.validateEnvironment(w, r, p.WorkspaceUUID); err != nil {
			return err
		}
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
	if r.Method == http.MethodPost && !settings && !strings.Contains(r.URL.Path, "/pairings") {
		if err := h.requireEnvironment(upstream, p.WorkspaceUUID, chi.URLParam(r, "channel_id")); err != nil {
			return err
		}
	}
	response, err := h.client.Do(upstream)
	if err != nil {
		return unavailable(err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 || response.StatusCode == 401 || response.StatusCode == 403 {
		return unavailable(nil)
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

func (h *handler) endpoint(r *http.Request, settings bool) (*url.URL, error) {
	endpoint, err := url.Parse(h.cfg.URL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || h.cfg.Token == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, unavailable(err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/managed-channels"
	if settings {
		endpoint.Path += "/settings"
	}
	if channelID := chi.URLParam(r, "channel_id"); channelID != "" {
		endpoint.Path += "/" + url.PathEscape(channelID)
		if strings.HasSuffix(r.URL.Path, "/qr") {
			endpoint.Path += "/qr"
		}
		if strings.Contains(r.URL.Path, "/pairings") {
			endpoint.Path += "/pairings"
		}
		if pairingID := chi.URLParam(r, "pairing_id"); pairingID != "" {
			endpoint.Path += "/" + url.PathEscape(pairingID)
		}
	}
	endpoint.RawQuery = ""
	return endpoint, nil
}
