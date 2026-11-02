package platformadmin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor"`
}
type Member struct {
	UUID  string `json:"uuid"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
type Workspace struct {
	UUID      string `json:"uuid"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	Archived  bool   `json:"archived"`
}

func readPage(r *http.Request) (db.PlatformAdminPageQuery, error) {
	v := r.URL.Query()
	q := db.PlatformAdminPageQuery{Query: strings.TrimSpace(v.Get("query")), After: v.Get("cursor"), Limit: 20, OrganizationUUID: chi.URLParam(r, "organizationUuid")}
	if utf8.RuneCountInString(q.Query) > 200 {
		return q, invalid("搜索内容过长")
	}
	if q.After != "" {
		if _, err := uuid.Parse(q.After); err != nil {
			return q, invalid("分页游标无效")
		}
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return q, invalid("每页数量必须在 1 至 100 之间")
		}
		q.Limit = n
	}
	q.Limit++
	return q, nil
}

func writePage[T, U any](w http.ResponseWriter, rows []T, limit int, convert func(T) U, key func(T) string) {
	out := page[U]{Data: make([]U, 0, min(len(rows), limit-1))}
	if len(rows) >= limit {
		out.NextCursor = key(rows[limit-2])
		rows = rows[:limit-1]
	}
	for _, row := range rows {
		out.Data = append(out.Data, convert(row))
	}
	respond(w, http.StatusOK, out)
}

func (h *Handler) accounts(w http.ResponseWriter, r *http.Request) {
	query, err := readPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if utf8.RuneCountInString(query.Query) < 2 {
		h.fail(w, r, invalid("请输入至少两个字符搜索账号"))
		return
	}
	actor := actorFromRequest(r)
	h.mu.Lock()
	now := time.Now()
	last := h.lookups[actor.AccountUUID]
	for account, stamp := range h.lookups {
		if now.Sub(stamp) > time.Minute {
			delete(h.lookups, account)
		}
	}
	allowed := now.Sub(last) >= 250*time.Millisecond
	if allowed {
		h.lookups[actor.AccountUUID] = now
	}
	h.mu.Unlock()
	if !allowed {
		h.fail(w, r, ErrRateLimited)
		return
	}
	rows, err := h.service.db.ListPlatformAdminAccounts(r.Context(), query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, rows, query.Limit, func(a db.PlatformAdminAccount) Account { return Account{a.UUID, a.Name, a.Email} }, func(a db.PlatformAdminAccount) string { return a.UUID })
}

func (h *Handler) organizations(w http.ResponseWriter, r *http.Request) {
	query, err := readPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	rows, err := h.service.db.ListPlatformAdminOrganizations(r.Context(), query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, rows, query.Limit, organizationDTO, func(a db.PlatformAdminOrganization) string { return a.UUID })
}

func (h *Handler) organization(w http.ResponseWriter, r *http.Request) {
	row, err := h.service.db.GetPlatformAdminOrganization(r.Context(), chi.URLParam(r, "organizationUuid"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	respond(w, http.StatusOK, organizationDTO(row))
}

func (h *Handler) members(w http.ResponseWriter, r *http.Request) {
	query, err := readPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	rows, err := h.service.db.ListPlatformAdminMembers(r.Context(), query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, rows, query.Limit, func(a db.PlatformAdminMember) Member { return Member{a.UUID, a.Email, a.Name, a.Role} }, func(a db.PlatformAdminMember) string { return a.UUID })
}

func (h *Handler) workspaces(w http.ResponseWriter, r *http.Request) {
	query, err := readPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	rows, err := h.service.db.ListPlatformAdminWorkspaces(r.Context(), query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, rows, query.Limit, func(a db.PlatformAdminWorkspace) Workspace {
		return Workspace{a.UUID, a.ExternalID, a.Name, a.IsDefault, a.Archived}
	}, func(a db.PlatformAdminWorkspace) string { return a.UUID })
}
