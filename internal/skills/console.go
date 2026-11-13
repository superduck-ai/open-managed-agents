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

func (h *Handler) consoleWrap(next httpapi.Endpoint) http.HandlerFunc {
	return h.errorAdapter.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		return mapConsoleSkillError(next(w, r))
	})
}

func (h *Handler) ConsoleRoutes() http.Handler {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return h.consoleWrap(func(w http.ResponseWriter, r *http.Request) error {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok || principal.CredentialType != auth.CredentialTypePlatformSession || principal.WorkspaceUUID == "" {
				return errConsoleSkillDenied
			}
			r = r.WithContext(context.WithValue(r.Context(), consoleSkillContextKey{}, true))
			next.ServeHTTP(w, r)
			return nil
		})
	})
	router.Get("/", h.consoleWrap(h.consoleCatalog))
	router.Get("/{skill_id}/versions/{version}/preview", h.consoleWrap(h.consoleContent))
	router.Post("/inspect", h.consoleWrap(h.inspectConsolePackage))
	router.Post("/", h.consoleWrap(h.create))
	router.Post("/{skill_id}/versions", h.consoleWrap(h.createVersionRoute))
	router.Delete("/{skill_id}", h.consoleWrap(h.deleteRoute))
	router.Patch("/{skill_id}/icon", h.consoleWrap(h.changeConsoleIcon))
	return router
}

type consoleSkillPreviewResponse struct {
	systemPackageResponse
	ID            string `json:"id"`
	DisplayTitle  string `json:"display_title"`
	IconKey       string `json:"icon_key"`
	Source        string `json:"source"`
	LatestVersion string `json:"latest_version"`
	UpdatedAt     string `json:"updated_at"`
}

func (h *Handler) consoleContent(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	skillID, version := chi.URLParam(r, "skill_id"), chi.URLParam(r, "version")
	var pkg skillPackage
	var bucket, key string
	var meta *skillResponse
	if systemSkill, isSystem, err := h.getAvailableSystemSkill(r.Context(), skillID); err != nil {
		return err
	} else if isSystem {
		if !systemSkill.Listed {
			return skillNotFound(skillID, nil)
		}
		record, err := h.db.GetSystemSkillVersion(r.Context(), skillID, version)
		if err != nil {
			return err
		}
		pkg = skillPackage{Name: record.Name, Description: record.Description, Directory: record.Directory, Size: record.SizeBytes, SHA256: record.SHA256}
		bucket, key = record.S3Bucket, record.S3Key
		item := responseFromSystemSkill(systemSkill)
		meta = &item
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
	if r.URL.Query().Get("download") == "1" || r.URL.Query().Get("path") != "" {
		return h.servePackage(w, r, bucket, key, pkg, 30*1024*1024)
	}
	if meta == nil {
		skill, err := h.db.GetSkill(r.Context(), principal.WorkspaceUUID, skillID)
		if err != nil {
			return err
		}
		item := responseFromSkill(skill)
		meta = &item
	}
	loaded, err := h.loadPackage(r.Context(), bucket, key, pkg, 30*1024*1024)
	if err != nil {
		return err
	}
	preview, err := systemPackageDTO(loaded)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httpapi.WriteJSON(w, http.StatusOK, consoleSkillPreviewResponse{
		systemPackageResponse: preview,
		ID:                    meta.ID,
		DisplayTitle:          meta.DisplayTitle,
		IconKey:               meta.IconKey,
		Source:                meta.Source,
		LatestVersion:         meta.LatestVersion,
		UpdatedAt:             meta.UpdatedAt,
	})
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
		return errInvalidSkillFilter
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
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1024), &input, json.RejectUnknownMembers(true)); err != nil {
		return errInvalidSkillIconInput
	}
	key, err := requireSkillIconKey(input.IconKey)
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
