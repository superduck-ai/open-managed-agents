package channels

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

func newLocalHandler(service *localService, logger *slog.Logger) *localHandler {
	return &localHandler{service: service, logger: logging.LoggerOrDefault(logger)}
}

func (h *localHandler) agentRouter() http.Handler {
	router := chi.NewRouter()
	wrap := httpapi.NewErrorAdapter(h.logger).Wrap
	router.Get("/", wrap(h.list))
	router.Post("/", wrap(h.create))
	router.Get("/settings", wrap(h.settings))
	router.Post("/settings", wrap(h.saveSettings))
	router.Post("/{channel_id}", wrap(h.update))
	router.Delete("/{channel_id}", wrap(h.remove))
	router.Get("/{channel_id}/qr", wrap(h.qr))
	router.Post("/{channel_id}/qr", wrap(h.qr))
	router.Get("/{channel_id}/pairings", wrap(h.pairings))
	router.Post("/{channel_id}/pairings/{pairing_id}", wrap(h.decidePairing))
	return router
}

func (h *localHandler) workspaceRouter() http.Handler {
	router := chi.NewRouter()
	wrap := httpapi.NewErrorAdapter(h.logger).Wrap
	router.Get("/", wrap(h.listWorkspace))
	router.Post("/credentials/check", wrap(h.checkAppCredentials))
	router.Post("/authorizations", wrap(h.beginAuthorization))
	router.Get("/authorizations/{authorization_id}", wrap(h.authorizationStatus))
	return router
}

func (h *localHandler) principal(r *http.Request, mutating bool) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.WorkspaceUUID == "" || principal.OrganizationUUID == "" {
		return auth.Principal{}, unauthenticated()
	}
	if mutating {
		if err := rejectBrowserCSRF(r, principal); err != nil {
			return auth.Principal{}, err
		}
	}
	return principal, nil
}

func (h *localHandler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	rows, err := h.service.list(r.Context(), principal, chi.URLParam(r, "agent_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": rows})
	return nil
}

func (h *localHandler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	input, err := decodeChannelMutation(w, r)
	if err != nil {
		return err
	}
	view, err := h.service.create(r.Context(), principal, chi.URLParam(r, "agent_id"), input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *localHandler) update(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	input, err := decodeChannelMutation(w, r)
	if err != nil {
		return err
	}
	view, err := h.service.update(r.Context(), principal, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id"), input)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *localHandler) remove(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	if err := h.service.remove(r.Context(), principal, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id")); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	return nil
}

func (h *localHandler) settings(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	value, err := h.service.environment(r.Context(), principal, chi.URLParam(r, "agent_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
	return nil
}

func (h *localHandler) saveSettings(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	var input environmentSettings
	if err := decodeStrictJSON(w, r, 32<<10, &input); err != nil {
		return invalidEnvironment(err)
	}
	if input.EnvironmentID == nil {
		return environmentRequired()
	}
	value, err := h.service.saveEnvironment(r.Context(), principal, chi.URLParam(r, "agent_id"), *input.EnvironmentID)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, value)
	return nil
}

func (h *localHandler) pairings(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	rows, err := h.service.listPairings(r.Context(), principal, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": rows})
	return nil
}

func (h *localHandler) decidePairing(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	var input channelMutation
	if err := decodeStrictJSON(w, r, 8<<10, &input); err != nil {
		return invalidChannelUpdate(err)
	}
	status := ""
	if input.Status != nil {
		status = *input.Status
	}
	rows, err := h.service.decidePairing(r.Context(), principal, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id"), chi.URLParam(r, "pairing_id"), status)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": rows})
	return nil
}

func (h *localHandler) listWorkspace(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	rows, err := h.service.workspaceChannels(r.Context(), principal)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, workspaceChannelPage{Data: rows})
	return nil
}

func (h *localHandler) checkAppCredentials(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	var input appCredentialCheck
	if err := decodeStrictJSON(w, r, 8<<10, &input); err != nil {
		return invalidCredentialCheckBody(err)
	}
	appID := strings.TrimSpace(input.AppID)
	if !feishuAppIDPattern.MatchString(appID) || input.AppSecret == "" {
		return invalidAppCredentials()
	}
	passed, err := h.service.credentialsMatch(r.Context(), appID, input.AppSecret)
	if err != nil {
		h.logger.WarnContext(r.Context(), "feishu app credential check failed", "workspace_id", principal.WorkspaceUUID, "error", err)
		return credentialCheckUnavailable(err)
	}
	if !passed {
		return invalidAppCredentials()
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (h *localHandler) qr(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, r.Method != http.MethodGet)
	if err != nil {
		return err
	}
	view, err := h.service.qrStatus(r.Context(), principal, chi.URLParam(r, "agent_id"), chi.URLParam(r, "channel_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *localHandler) beginAuthorization(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, true)
	if err != nil {
		return err
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeStrictJSON(w, r, 8<<10, &input); err != nil {
		return invalidChannelName()
	}
	view, err := h.service.beginAuthorization(r.Context(), principal, input.Name)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *localHandler) authorizationStatus(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r, false)
	if err != nil {
		return err
	}
	view, err := h.service.authorizationStatus(r.Context(), principal, chi.URLParam(r, "authorization_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *localHandler) unsupported(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.principal(r, r.Method != http.MethodGet); err != nil {
		return err
	}
	return channelCapabilityUnsupported()
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func decodeChannelMutation(w http.ResponseWriter, r *http.Request) (channelMutation, error) {
	var input channelMutation
	if err := decodeStrictJSON(w, r, 32<<10, &input); err != nil {
		return channelMutation{}, invalidChannelUpdate(err)
	}
	if mountRequested(input.MemoryStoreID) || mountRequested(input.VaultID) || mountRequested(input.GitRepository) {
		return channelMutation{}, channelMountsUnsupported()
	}
	return input, nil
}

func mountRequested(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))
	return trimmed != "" && trimmed != "null"
}
