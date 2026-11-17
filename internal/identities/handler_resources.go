package identities

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type configBody struct {
	Revision       *int64          `json:"revision"`
	Env            json.RawMessage `json:"env"`
	VaultMounts    json.RawMessage `json:"vault_mounts"`
	DisabledVaults json.RawMessage `json:"disabled_vaults"`
}

type pairingBody struct {
	Code         string `json:"code"`
	PairingID    string `json:"pairing_id"`
	IdentityID   string `json:"identity_id"`
	AgentID      string `json:"agent_id"`
	AgentVersion *int   `json:"agent_version"`
	Revision     int64  `json:"revision"`
}

func (h *Handler) configs(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	limit, err := queryLimit(r)
	if err != nil {
		return err
	}
	rows, next, err := h.service.ListConfigs(r.Context(), principal, chi.URLParam(r, "identity_id"), r.URL.Query().Get("agent_id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, configView(row))
	}
	httpapi.WriteJSON(w, http.StatusOK, pageBody(data, next))
	return nil
}

func (h *Handler) getConfig(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	row, err := h.service.GetConfig(r.Context(), principal, chi.URLParam(r, "identity_id"), chi.URLParam(r, "agent_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, configView(row))
	return nil
}

func (h *Handler) putConfig(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[configBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	patch, expected, err := parseConfigPatch(*body)
	if err != nil {
		return err
	}
	row, err := h.service.PutConfig(r.Context(), principal, chi.URLParam(r, "identity_id"), chi.URLParam(r, "agent_id"), expected, patch)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, configView(row))
	return nil
}

func (h *Handler) effective(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	version := 0
	if raw := r.URL.Query().Get("agent_version"); raw != "" {
		parsed, parseErr := queryLimitFrom(raw)
		if parseErr != nil {
			return invalid("agent_version 无效", parseErr)
		}
		version = parsed
	}
	view, err := h.service.Effective(r.Context(), principal, chi.URLParam(r, "identity_id"), chi.URLParam(r, "agent_id"), version)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) pairings(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	limit, err := queryLimit(r)
	if err != nil {
		return err
	}
	rows, next, err := h.service.ListPairings(r.Context(), principal, r.URL.Query().Get("channel_id"), r.URL.Query().Get("status"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, pageBody(rows, next))
	return nil
}

func (h *Handler) pairing(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	view, err := h.service.GetPairing(r.Context(), principal, chi.URLParam(r, "pairing_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) completePairing(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[pairingBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	view, err := h.service.CompletePairing(r.Context(), principal, PairingWrite{
		Code: body.Code, PairingID: body.PairingID, IdentityID: body.IdentityID, AgentID: body.AgentID, AgentVersion: body.AgentVersion,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) updatePairing(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[pairingBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	view, err := h.service.UpdatePairing(r.Context(), principal, chi.URLParam(r, "pairing_id"), PairingWrite{
		IdentityID: body.IdentityID, AgentID: body.AgentID, AgentVersion: body.AgentVersion, Revision: body.Revision,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) unbindPairing(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[pairingBody](w, r, 1<<20)
	if err != nil {
		return invalid("请求格式无效", err)
	}
	view, err := h.service.UnbindPairing(r.Context(), principal, chi.URLParam(r, "pairing_id"), body.Revision)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, view)
	return nil
}

func (h *Handler) agentBindings(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	rows, err := h.service.ListAgentBindings(r.Context(), principal, chi.URLParam(r, "agent_id"))
	if err != nil {
		return err
	}
	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, map[string]any{
			"identity_id": row.IdentityExternalID, "display_name": row.DisplayName, "channel_id": row.ChannelExternalID,
			"scope_kind": row.ScopeKind, "remote_id": row.RemoteID, "pairing_id": row.PairingExternalID, "status": row.Status,
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
	return nil
}

func queryLimitFrom(raw string) (int, error) {
	return strconvAtoi(raw)
}

func strconvAtoi(raw string) (int, error) {
	value := 0
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0, invalid("数字无效", nil)
		}
		value = value*10 + int(char-'0')
	}
	return value, nil
}
