package skills

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type consoleSkillContextKey struct{}

func isConsoleSkillRequest(r *http.Request) bool {
	return r.Context().Value(consoleSkillContextKey{}) == true
}

func (h *Handler) ConsoleRoutes() http.Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return h.systemWrap(func(w http.ResponseWriter, r *http.Request) error {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal.CredentialType != auth.CredentialTypePlatformSession || principal.WorkspaceUUID == "" {
				return consoleSkillDenied()
			}
			r = r.WithContext(context.WithValue(r.Context(), consoleSkillContextKey{}, true))
			next.ServeHTTP(w, r)
			return nil
		})
	})
	router.Get("/", h.systemWrap(h.consoleCatalog))
	router.Get("/{skill_id}/versions/{version}/preview", h.systemWrap(h.consoleContent))
	router.Post("/inspect", h.systemWrap(h.inspectConsolePackage))
	router.Post("/", h.systemWrap(h.create))
	router.Post("/{skill_id}/versions", h.systemWrap(h.createVersionRoute))
	router.Delete("/{skill_id}", h.systemWrap(h.deleteRoute))
	router.Patch("/{skill_id}/icon", h.systemWrap(h.changeConsoleIcon))
	return router
}

func (h *Handler) consoleContent(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	skillID, version := chi.URLParam(r, "skill_id"), chi.URLParam(r, "version")
	var pkg skillPackage
	var bucket, key string
	if _, builtin, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return err
	} else if builtin {
		record, err := h.db.GetBuiltinSkillVersion(r.Context(), skillID, version)
		if err != nil {
			return err
		}
		pkg = skillPackage{Name: record.Name, Description: record.Description, Directory: record.Directory, Size: record.SizeBytes, SHA256: record.SHA256}
		bucket, key = record.S3Bucket, record.S3Key
	} else {
		resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceUUID, skillID, version)
		if err != nil {
			return err
		}
		record, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceUUID, skillID, resolved)
		if err != nil {
			return err
		}
		pkg = skillPackage{Name: record.Name, Description: record.Description, Directory: record.Directory, Size: record.SizeBytes, SHA256: record.SHA256}
		bucket, key = record.S3Bucket, record.S3Key
	}
	result, err := h.packagePreview(r.Context(), bucket, key, pkg, 30*1024*1024)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) consoleCatalog(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := systemPaging(r)
	if err != nil {
		return err
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	query := db.SkillCatalogQuery{WorkspaceUUID: principal.WorkspaceUUID, Query: strings.TrimSpace(r.URL.Query().Get("query")), Source: r.URL.Query().Get("source"), Sort: r.URL.Query().Get("sort"), Limit: limit, Offset: offset}
	if len(query.Query) > 256 || (query.Source != "" && query.Source != "custom" && query.Source != "anthropic") || (query.Sort != "" && query.Sort != "name" && query.Sort != "updated") {
		return systemInvalid("技能筛选条件无效")
	}
	rows, counts, err := h.db.SkillCatalog(r.Context(), query)
	if err != nil {
		return err
	}
	data := make([]consoleCatalogSkill, 0, len(rows))
	for _, row := range rows {
		data = append(data, consoleCatalogSkill{skillResponse: skillResponse{ID: row.ExternalID, DisplayTitle: row.DisplayTitle, IconKey: row.IconKey, LatestVersion: row.LatestVersion, Source: row.Source, Type: "skill", CreatedAt: formatTime(row.CreatedAt), UpdatedAt: formatTime(row.UpdatedAt)}, Description: row.Description})
	}
	total := counts.All
	if query.Source == "custom" {
		total = counts.Custom
	}
	if query.Source == "anthropic" {
		total = counts.Builtin
	}
	httpapi.WriteJSON(w, 200, struct {
		Data       []consoleCatalogSkill `json:"data"`
		TotalCount int                   `json:"total_count"`
		Counts     consoleSkillCounts    `json:"counts"`
	}{data, total, consoleSkillCounts{counts.All, counts.Builtin, counts.Custom}})
	return nil
}

type consoleSkillCounts struct {
	All     int `json:"all"`
	Builtin int `json:"builtin"`
	Custom  int `json:"custom"`
}

type consoleCatalogSkill struct {
	skillResponse
	Description string `json:"description"`
}

func (h *Handler) inspectConsolePackage(w http.ResponseWriter, r *http.Request) error {
	pkg, err := readSkillPackageOptions(w, r, MaxSkillPackageBytes, true)
	if err != nil {
		return err
	}
	preview, err := systemPackageDTO(pkg)
	if err != nil {
		return err
	}
	target := r.FormValue("skill_id")
	if target != "" {
		principal, _ := auth.PrincipalFromContext(r.Context())
		skill, err := h.db.GetSkill(r.Context(), principal.WorkspaceUUID, target)
		if err != nil {
			return err
		}
		previous, err := h.db.GetLatestSkillVersion(r.Context(), principal.WorkspaceUUID, target)
		if err != nil {
			return err
		}
		if previous.Name != pkg.Name {
			return skillNameImmutable()
		}
		preview.Name = valueOrEmpty(skill.DisplayTitle)
	}
	httpapi.WriteJSON(w, 200, preview)
	return nil
}

func (h *Handler) changeConsoleIcon(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		IconKey string `json:"icon_key"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1024), &input); err != nil {
		return systemInvalid("图标参数无效")
	}
	key, err := skillIconKey(input.IconKey)
	if err != nil {
		return err
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	skill, err := h.db.UpdateSkillIcon(r.Context(), principal.WorkspaceUUID, chi.URLParam(r, "skill_id"), key)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, responseFromSkill(skill))
	return nil
}
