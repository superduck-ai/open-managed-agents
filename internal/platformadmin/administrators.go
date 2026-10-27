package platformadmin

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) administrators(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.Administrators(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, Account{UUID: row.UUID, Name: row.Name, Email: row.Email})
	}
	respond(w, http.StatusOK, page[Account]{Data: accounts})
}

func (h *Handler) addPlatformAdministrator(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[struct {
		AccountUUID string `json:"account_uuid"`
	}](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.changePlatformAdministrator(w, r, input.AccountUUID, true)
}

func (h *Handler) removePlatformAdministrator(w http.ResponseWriter, r *http.Request) {
	h.changePlatformAdministrator(w, r, chi.URLParam(r, "accountUuid"), false)
}

func (h *Handler) changePlatformAdministrator(w http.ResponseWriter, r *http.Request, accountUUID string, granted bool) {
	if err := h.service.ChangeAdministrator(r.Context(), actorFromRequest(r), accountUUID, granted); err != nil {
		h.fail(w, r, err)
		return
	}
	respond(w, http.StatusOK, struct {
		AccountUUID string `json:"account_uuid"`
		Granted     bool   `json:"granted"`
	}{accountUUID, granted})
}
