package identities

import (
	"encoding/json/jsontext"
	"net/http"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) directory(w http.ResponseWriter, r *http.Request) error {
	principal, err := h.principal(r)
	if err != nil {
		return err
	}
	query := r.URL.Query()
	page := 1
	if raw := query.Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 || page > 1_000_000 {
			return invalid("页码无效", err)
		}
	}
	result, err := h.service.identityDirectory(r.Context(), db.IdentityDirectoryParams{
		OrganizationUUID: principal.OrganizationUUID,
		WorkspaceUUID:    principal.WorkspaceUUID,
		Search:           strings.TrimSpace(query.Get("q")),
		Status:           query.Get("status"),
		AgentID:          query.Get("agent_id"),
		Sort:             query.Get("sort"),
		Offset:           (page - 1) * pageDefault,
		Limit:            pageDefault,
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, jsontext.Value(result))
	return nil
}
