package identities

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"log/slog"
)

type Handler struct {
	service *Service
	adapter *httpapi.ErrorAdapter
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, adapter: httpapi.NewErrorAdapter(logger)}
}

func (h *Handler) RegisterConsole(router chi.Router) {
	base := "/workspaces/{workspaceId}"
	h.mount(router, base)
	router.Get(base+"/identities/directory", h.adapter.Wrap(h.directory))
	router.Post(base+"/identities", h.adapter.Wrap(h.create))
	router.Patch(base+"/identities/{identity_id}", h.adapter.Wrap(h.update))
	router.Delete(base+"/identities/{identity_id}", h.adapter.Wrap(h.remove))
	router.Post(base+"/identities/{identity_id}/clear", h.adapter.Wrap(h.clear))
	router.Get(base+"/identities/{identity_id}/sessions", h.adapter.Wrap(h.sessions))
	router.Get(base+"/identities/{identity_id}/configs", h.adapter.Wrap(h.configs))
	router.Get(base+"/identities/{identity_id}/agents/{agent_id}/config", h.adapter.Wrap(h.getConfig))
	router.Put(base+"/identities/{identity_id}/agents/{agent_id}/config", h.adapter.Wrap(h.putConfig))
	router.Get(base+"/identities/{identity_id}/agents/{agent_id}/effective", h.adapter.Wrap(h.effective))
	router.Get(base+"/identity_cleanups/{cleanup_id}", h.adapter.Wrap(h.cleanup))
	router.Get(base+"/agents/{agent_id}/identity_bindings", h.adapter.Wrap(h.agentBindings))
	router.Post(base+"/channel_pairings", h.adapter.Wrap(h.completePairing))
	router.Patch(base+"/channel_pairings/{pairing_id}", h.adapter.Wrap(h.updatePairing))
}

func (h *Handler) RegisterV1(router chi.Router) {
	h.mount(router, "")
}

func (h *Handler) mount(router chi.Router, base string) {
	router.Get(base+"/identities", h.adapter.Wrap(h.list))
	router.Get(base+"/identities/stats", h.adapter.Wrap(h.stats))
	router.Get(base+"/identities/{identity_id}", h.adapter.Wrap(h.get))
	router.Post(base+"/identities/{identity_id}/enable", h.adapter.Wrap(h.enable))
	router.Post(base+"/identities/{identity_id}/disable", h.adapter.Wrap(h.disable))
	router.Get(base+"/identities/{identity_id}/agents", h.adapter.Wrap(h.agents))
	router.Get(base+"/channel_pairings", h.adapter.Wrap(h.pairings))
	router.Get(base+"/channel_pairings/{pairing_id}", h.adapter.Wrap(h.pairing))
	router.Delete(base+"/channel_pairings/{pairing_id}", h.adapter.Wrap(h.unbindPairing))
}

func (h *Handler) principal(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.OrganizationUUID == "" || principal.WorkspaceUUID == "" {
		return auth.Principal{}, unauthenticated()
	}
	if org := chi.URLParam(r, "orgUuid"); org != "" && org != principal.OrganizationUUID {
		return auth.Principal{}, notFound("执行身份不存在", nil)
	}
	workspaceID := chi.URLParam(r, "workspaceId")
	if workspaceID != "" && workspaceID != "default" && workspaceID != principal.WorkspaceUUID && workspaceID != principal.WorkspaceExternalID {
		return auth.Principal{}, notFound("执行身份不存在", nil)
	}
	return principal, nil
}

type identityBody struct {
	Namespace       *string `json:"namespace"`
	SubjectID       *string `json:"external_subject_id"`
	DisplayName     *string `json:"display_name"`
	Enabled         *bool   `json:"enabled"`
	AutoMemoryStore *bool   `json:"auto_memory_store"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[identityBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	row, err := h.service.CreateIdentity(r.Context(), principal, identityInput(*body))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, identityView(row))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	limit, err := queryLimit(r)
	if err != nil {
		return err
	}
	rows, next, err := h.service.ListIdentities(r.Context(), principal, r.URL.Query().Get("cursor"), r.URL.Query().Get("status"), r.URL.Query().Get("external_subject_id"), r.URL.Query().Get("namespace"), r.URL.Query().Get("q"), limit)
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, identityView(row))
	}
	httpapi.WriteJSON(w, http.StatusOK, pageBody(data, next))
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	row, err := h.service.GetIdentity(r.Context(), principal, chi.URLParam(r, "identity_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, identityView(row))
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[identityBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	row, err := h.service.UpdateIdentity(r.Context(), principal, chi.URLParam(r, "identity_id"), identityInput(*body))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, identityView(row))
	return nil
}

func (h *Handler) enable(w http.ResponseWriter, r *http.Request) error {
	return h.setEnabled(w, r, true)
}

func (h *Handler) disable(w http.ResponseWriter, r *http.Request) error {
	return h.setEnabled(w, r, false)
}

func (h *Handler) setEnabled(w http.ResponseWriter, r *http.Request, enabled bool) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	var row db.IdentityRecord
	if enabled {
		row, err = h.service.EnableIdentity(r.Context(), principal, chi.URLParam(r, "identity_id"))
	} else {
		row, err = h.service.DisableIdentity(r.Context(), principal, chi.URLParam(r, "identity_id"))
	}
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, identityView(row))
	return nil
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	if err := h.service.DeleteIdentity(r.Context(), principal, chi.URLParam(r, "identity_id")); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": chi.URLParam(r, "identity_id"), "deleted": true})
	return nil
}

func (h *Handler) clear(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	result, err := h.service.ClearIdentity(r.Context(), principal, chi.URLParam(r, "identity_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": result.CleanupID, "state": result.State, "result": result.Outcome})
	return nil
}

func (h *Handler) cleanup(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	result, err := h.service.GetCleanup(r.Context(), principal, chi.URLParam(r, "cleanup_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"id": result.CleanupID, "state": result.State, "result": result.Outcome})
	return nil
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	identityID := r.URL.Query().Get("identity_id")
	if identityID == "" {
		return invalid("请指定 identity_id", nil)
	}
	stats, err := h.service.Stats(r.Context(), principal, identityID, r.URL.Query().Get("window"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"identity_id": identityID, "window": windowLabel(r.URL.Query().Get("window")), "sessions": stats.Sessions, "agents": stats.Agents, "last_used_at": formatTime(stats.LastUsedAt)})
	return nil
}

func (h *Handler) agents(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	query := r.URL.Query()
	window := query.Get("window")
	page, err := identityPageNumber(query.Get("page"))
	if err != nil {
		return err
	}
	includeChannels := query.Get("include_channels") == "true"
	rows, channels, hasMore, err := h.service.IdentityAgents(r.Context(), principal, chi.URLParam(r, "identity_id"), window, page, includeChannels)
	if err != nil {
		return err
	}
	type agentChannel struct {
		ID            string `json:"id"`
		Type          string `json:"channel_type"`
		Name          string `json:"name"`
		BindingStatus string `json:"binding_status"`
	}
	byAgent := make(map[string][]agentChannel)
	for _, channel := range channels {
		byAgent[channel.AgentExternalID] = append(byAgent[channel.AgentExternalID], agentChannel{
			ID: channel.ChannelExternalID, Type: channel.ChannelType, Name: channel.Name, BindingStatus: channel.BindingStatus,
		})
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		view := map[string]any{"agent_id": row.AgentExternalID, "sessions": row.Sessions, "last_used_at": formatTime(row.LastUsedAt)}
		if includeChannels {
			view["channels"] = append([]agentChannel{}, byAgent[row.AgentExternalID]...)
		}
		data = append(data, view)
	}
	body := map[string]any{"data": data, "window": windowLabel(window)}
	if hasMore {
		body["next_page"] = page + 1
	}
	httpapi.WriteJSON(w, http.StatusOK, body)
	return nil
}

func (h *Handler) sessions(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	rows, err := h.service.ListRecoverableSessions(r.Context(), principal, chi.URLParam(r, "identity_id"), r.URL.Query().Get("scope_key"), r.URL.Query().Get("personal_vault_id"))
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"session_id": row.SessionExternalID, "agent_id": row.AgentExternalID, "scope_key": row.ScopeKey,
			"scope_kind": row.ScopeKind, "personal_vault_id": row.PersonalVaultID, "binding_revision": row.BindingRevision,
			"config_revision": row.ConfigRevision, "status": row.SessionStatus,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

func identityInput(body identityBody) IdentityInput {
	input := IdentityInput{Enabled: body.Enabled, AutoMemoryStore: body.AutoMemoryStore, NamespaceProvided: body.Namespace != nil}
	if body.Namespace != nil {
		input.Namespace = *body.Namespace
	}
	if body.SubjectID != nil {
		input.SubjectID = *body.SubjectID
	}
	if body.DisplayName != nil {
		input.DisplayName = *body.DisplayName
	}
	return input
}

func queryLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return pageDefault, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 {
		return 0, invalid("limit 无效", err)
	}
	return limit, nil
}

func pageBody(data []map[string]any, next string) map[string]any {
	body := map[string]any{"data": data}
	if next != "" {
		body["next_cursor"] = next
	}
	return body
}

func windowLabel(window string) string {
	if window == "" {
		return "30d"
	}
	return window
}

func formatTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseConfigPatch(body configBody) (ConfigPatch, int64, error) {
	patch := ConfigPatch{}
	var expected int64
	if body.Revision != nil {
		expected = *body.Revision
	}
	envPatch, err := parseMapPatch(body.Env)
	if err != nil {
		return ConfigPatch{}, 0, err
	}
	patch.EnvSet = envPatch.set
	patch.EnvClear = envPatch.clear
	patch.Env = envPatch.values
	vaultPatch, err := parseListPatch(body.VaultMounts)
	if err != nil {
		return ConfigPatch{}, 0, err
	}
	patch.VaultsSet = vaultPatch.set
	patch.VaultsClear = vaultPatch.clear
	patch.Vaults = vaultPatch.values
	disabledPatch, err := parseListPatch(body.DisabledVaults)
	if err != nil {
		return ConfigPatch{}, 0, err
	}
	patch.DisabledSet = disabledPatch.set && !disabledPatch.clear
	if disabledPatch.clear {
		patch.DisabledSet = true
		patch.Disabled = []string{}
	} else {
		patch.Disabled = disabledPatch.values
	}
	return patch, expected, nil
}

type mapPatch struct {
	set    bool
	clear  bool
	values map[string]*string
}

type listPatch struct {
	set    bool
	clear  bool
	values []string
}

func parseMapPatch(raw json.RawMessage) (mapPatch, error) {
	if len(raw) == 0 {
		return mapPatch{}, nil
	}
	if string(raw) == "null" {
		return mapPatch{clear: true}, nil
	}
	var values map[string]*string
	if err := json.Unmarshal(raw, &values); err != nil {
		return mapPatch{}, invalid("env 必须是对象", err)
	}
	return mapPatch{set: true, values: values}, nil
}

func parseListPatch(raw json.RawMessage) (listPatch, error) {
	if len(raw) == 0 {
		return listPatch{}, nil
	}
	if string(raw) == "null" {
		return listPatch{clear: true}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return listPatch{}, invalid("列表必须是字符串数组", err)
	}
	return listPatch{set: true, values: values}, nil
}
