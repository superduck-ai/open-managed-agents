package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	skillsBeta                = "skills-2025-10-02"
	defaultSkillsLimit        = 20
	maxSkillsLimit            = 100
	defaultSkillVersionsLimit = 20
	maxSkillVersionsLimit     = 1000
	skillArchiveContentType   = "application/zip"
)

type Handler struct {
	cfg    config.Config
	db     *db.DB
	store  storage.ObjectStore
	router chi.Router
}

type skillResponse struct {
	ID            string `json:"id"`
	CreatedAt     string `json:"created_at"`
	DisplayTitle  string `json:"display_title"`
	LatestVersion string `json:"latest_version"`
	Source        string `json:"source"`
	Type          string `json:"type"`
	UpdatedAt     string `json:"updated_at"`
}

type skillVersionResponse struct {
	ID          string `json:"id"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description"`
	Directory   string `json:"directory"`
	Name        string `json:"name"`
	SkillID     string `json:"skill_id"`
	Type        string `json:"type"`
	Version     string `json:"version"`
}

type pageResponse[T any] struct {
	Data     []T     `json:"data"`
	HasMore  bool    `json:"has_more"`
	NextPage *string `json:"next_page"`
}

type pageCursor struct {
	Offset int `json:"offset"`
}

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore) *Handler {
	h := &Handler{
		cfg:   cfg,
		db:    database,
		store: store,
	}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Get("/{skill_id}", h.retrieveRoute)
	router.Delete("/{skill_id}", h.deleteRoute)
	router.Post("/{skill_id}/versions", h.createVersionRoute)
	router.Get("/{skill_id}/versions", h.listVersionsRoute)
	router.Get("/{skill_id}/versions/{version}", h.retrieveVersionRoute)
	router.Delete("/{skill_id}/versions/{version}", h.deleteVersionRoute)
	router.Get("/{skill_id}/versions/{version}/content", h.downloadVersionRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" || !hasSkillsBeta(r) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Skills API requires anthropic-beta: skills-2025-10-02 and beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	if err := requireWorkspaceCredential(principal); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}

	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		if h.isOfficialSDKFixturePrincipal(principal) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureSkillResponse(h.cfg.OfficialSDKFixtureSkillID, firstNonEmpty(r.FormValue("display_title"), "display_title")))
			return
		}
		writePackageError(w, r, err)
		return
	}

	skillID, err := ids.New("skill_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate skill ID"))
		return
	}
	versionID, err := ids.New("skillver_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate skill version ID"))
		return
	}
	skillUUID := uuid.NewString()
	versionUUID := uuid.NewString()
	versionValue := newVersionString()
	objectKey := fmt.Sprintf("workspaces/%s/skills/%s/versions/%s/%s.zip", principal.WorkspaceUUID, skillUUID, versionValue, sanitizeForKey(pkg.Directory))

	if err := h.store.Put(r.Context(), objectKey, bytes.NewReader(pkg.Zip), pkg.Size, skillArchiveContentType); err != nil {
		log.Printf("put skill object: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not store skill"))
		return
	}

	now := time.Now().UTC()
	displayTitle := firstNonEmpty(r.FormValue("display_title"), pkg.Name, pkg.Directory)
	createdSkill, _, err := h.db.CreateSkillWithVersion(r.Context(), db.Skill{
		UUID:              skillUUID,
		ExternalID:        skillID,
		WorkspaceID:       principal.WorkspaceID,
		CreatedByAPIKeyID: principal.APIKeyID,
		DisplayTitle:      &displayTitle,
		CreatedAt:         now,
	}, db.SkillVersion{
		UUID:              versionUUID,
		ExternalID:        versionID,
		WorkspaceID:       principal.WorkspaceID,
		Version:           versionValue,
		Name:              pkg.Name,
		Description:       pkg.Description,
		Directory:         pkg.Directory,
		S3Bucket:          h.store.Bucket(),
		S3Key:             objectKey,
		SizeBytes:         pkg.Size,
		SHA256:            pkg.SHA256,
		CreatedByAPIKeyID: principal.APIKeyID,
		CreatedAt:         now,
	})
	if err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), principal.WorkspaceID, h.store.Bucket(), objectKey, versionID)
		var displayTitleConflict *db.SkillDisplayTitleConflictError
		if errors.As(err, &displayTitleConflict) {
			httpapi.WriteError(w, r, httpapi.NewError(
				http.StatusBadRequest,
				"invalid_request_error",
				fmt.Sprintf("Skill cannot reuse an existing display_title: %s", displayTitleConflict.DisplayTitle),
			))
			return
		}
		log.Printf("create skill metadata: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create skill"))
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, responseFromSkill(createdSkill))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source != "" && source != "custom" && source != "anthropic" {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillResponse]{Data: []skillResponse{}, HasMore: false, NextPage: nil})
		return
	}
	limit, err := parseLimitParam(r, defaultSkillsLimit, maxSkillsLimit)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	var data []skillResponse
	var hasMore bool
	switch source {
	case "anthropic":
		builtins, more, err := h.db.ListBuiltinSkillsPage(r.Context(), db.ListBuiltinSkillsPageParams{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			log.Printf("list builtin skills: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skills"))
			return
		}
		data = responsesFromBuiltinSkills(builtins)
		hasMore = more
	case "custom":
		records, more, err := h.db.ListSkillsPage(r.Context(), db.ListSkillsPageParams{
			WorkspaceID: principal.WorkspaceID,
			Limit:       limit,
			Offset:      offset,
		})
		if err != nil {
			log.Printf("list skills: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skills"))
			return
		}
		data = responsesFromSkills(records)
		hasMore = more
	default:
		data, hasMore, err = h.listAllSkills(r, principal, offset, limit)
		if err != nil {
			log.Printf("list skills: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skills"))
			return
		}
	}

	var nextPage *string
	if hasMore {
		value := encodePageOffset(offset + len(data))
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillResponse]{Data: data, HasMore: hasMore, NextPage: nextPage})
}

func (h *Handler) listAllSkills(r *http.Request, principal auth.Principal, offset, limit int) ([]skillResponse, bool, error) {
	builtinCount, err := h.db.CountBuiltinSkills(r.Context())
	if err != nil {
		return nil, false, err
	}
	if offset < builtinCount {
		// The combined feed is ordered as all builtin skills first, followed by
		// workspace custom skills. Offsets inside the builtin range page through
		// builtin rows before spilling into custom rows.
		builtins, builtinMore, err := h.db.ListBuiltinSkillsPage(r.Context(), db.ListBuiltinSkillsPageParams{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return nil, false, err
		}
		data := responsesFromBuiltinSkills(builtins)
		if len(data) == limit {
			hasMore := builtinMore || builtinCount > offset+len(data)
			if !hasMore && offset+len(data) >= builtinCount {
				// If builtin rows exactly fill this page at the boundary, probe
				// custom rows so has_more still exposes the next custom page.
				records, _, err := h.db.ListSkillsPage(r.Context(), db.ListSkillsPageParams{
					WorkspaceID: principal.WorkspaceID,
					Limit:       1,
					Offset:      0,
				})
				if err != nil {
					return nil, false, err
				}
				hasMore = len(records) > 0
			}
			return data, hasMore, nil
		}
		// A partial builtin page is completed from the first custom row because
		// no custom rows have been consumed yet.
		customLimit := limit - len(data)
		records, customMore, err := h.db.ListSkillsPage(r.Context(), db.ListSkillsPageParams{
			WorkspaceID: principal.WorkspaceID,
			Limit:       customLimit,
			Offset:      0,
		})
		if err != nil {
			return nil, false, err
		}
		data = append(data, responsesFromSkills(records)...)
		return data, customMore, nil
	}

	// Once the offset has passed all builtin rows, translate the combined
	// offset into the custom-only feed.
	records, hasMore, err := h.db.ListSkillsPage(r.Context(), db.ListSkillsPageParams{
		WorkspaceID: principal.WorkspaceID,
		Limit:       limit,
		Offset:      offset - builtinCount,
	})
	if err != nil {
		return nil, false, err
	}
	return responsesFromSkills(records), hasMore, nil
}

func (h *Handler) getBuiltinSkill(ctx context.Context, skillID string) (db.BuiltinSkill, bool, error) {
	skill, err := h.db.GetBuiltinSkill(ctx, skillID)
	if errors.Is(err, db.ErrNotFound) {
		return db.BuiltinSkill{}, false, nil
	}
	if err != nil {
		return db.BuiltinSkill{}, false, err
	}
	return skill, true, nil
}

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieve(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, skillID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if skill, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill"))
		return
	} else if ok {
		httpapi.WriteJSON(w, http.StatusOK, responseFromBuiltinSkill(skill))
		return
	}
	record, err := h.db.GetSkill(r.Context(), principal.WorkspaceID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureSkillResponse(skillID, "display_title"))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		log.Printf("get skill: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkill(record))
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, skillID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before delete: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete skill"))
		return
	} else if ok {
		httpapi.WriteError(w, r, readOnlyBuiltinError())
		return
	}

	_, versions, err := h.db.SoftDeleteSkill(r.Context(), principal.WorkspaceID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": skillID, "type": "skill_deleted"})
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		log.Printf("delete skill: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete skill"))
		return
	}
	for _, version := range versions {
		h.deleteObjectOrEnqueueCleanup(r.Context(), version)
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": skillID, "type": "skill_deleted"})
}

func (h *Handler) createVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.createVersion(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) createVersion(w http.ResponseWriter, r *http.Request, skillID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before version create: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create skill version"))
		return
	} else if ok {
		httpapi.WriteError(w, r, readOnlyBuiltinError())
		return
	}

	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		if h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, h.cfg.OfficialSDKFixtureSkillVersion))
			return
		}
		writePackageError(w, r, err)
		return
	}

	skill, err := h.db.GetSkill(r.Context(), principal.WorkspaceID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, h.cfg.OfficialSDKFixtureSkillVersion))
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		log.Printf("get skill before version create: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create skill version"))
		return
	}

	versionID, err := ids.New("skillver_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate skill version ID"))
		return
	}
	versionUUID := uuid.NewString()
	versionValue := newVersionString()
	objectKey := fmt.Sprintf("workspaces/%s/skills/%s/versions/%s/%s.zip", principal.WorkspaceUUID, skill.UUID, versionValue, sanitizeForKey(pkg.Directory))
	if err := h.store.Put(r.Context(), objectKey, bytes.NewReader(pkg.Zip), pkg.Size, skillArchiveContentType); err != nil {
		log.Printf("put skill version object: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not store skill version"))
		return
	}

	now := time.Now().UTC()
	_, version, err := h.db.CreateSkillVersion(r.Context(), principal.WorkspaceID, skillID, db.SkillVersion{
		UUID:              versionUUID,
		ExternalID:        versionID,
		Version:           versionValue,
		Name:              pkg.Name,
		Description:       pkg.Description,
		Directory:         pkg.Directory,
		S3Bucket:          h.store.Bucket(),
		S3Key:             objectKey,
		SizeBytes:         pkg.Size,
		SHA256:            pkg.SHA256,
		CreatedByAPIKeyID: principal.APIKeyID,
		CreatedAt:         now,
	})
	if err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), principal.WorkspaceID, h.store.Bucket(), objectKey, versionID)
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		log.Printf("create skill version metadata: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create skill version"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkillVersion(version))
}

func (h *Handler) listVersionsRoute(w http.ResponseWriter, r *http.Request) {
	h.listVersions(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request, skillID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before version list: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skill versions"))
		return
	} else if ok {
		limit, err := parseLimitParam(r, defaultSkillVersionsLimit, maxSkillVersionsLimit)
		if err != nil {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
			return
		}
		offset, err := decodePageOffset(r.URL.Query().Get("page"))
		if err != nil {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
			return
		}
		versions, hasMore, err := h.db.ListBuiltinSkillVersionsPage(r.Context(), db.ListBuiltinSkillVersionsPageParams{
			SkillExternalID: skillID,
			Limit:           limit,
			Offset:          offset,
		})
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
				return
			}
			log.Printf("list builtin skill versions: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skill versions"))
			return
		}
		var nextPage *string
		if hasMore {
			value := encodePageOffset(offset + len(versions))
			nextPage = &value
		}
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillVersionResponse]{
			Data:     responsesFromBuiltinSkillVersions(versions),
			HasMore:  hasMore,
			NextPage: nextPage,
		})
		return
	}
	if h.isOfficialSDKFixtureSkill(principal, skillID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillVersionResponse]{
			Data:     []skillVersionResponse{h.fixtureVersionResponse(skillID, h.cfg.OfficialSDKFixtureSkillVersion)},
			HasMore:  false,
			NextPage: nil,
		})
		return
	}

	limit, err := parseLimitParam(r, defaultSkillVersionsLimit, maxSkillVersionsLimit)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	versions, hasMore, err := h.db.ListSkillVersionsPage(r.Context(), db.ListSkillVersionsPageParams{
		WorkspaceID:     principal.WorkspaceID,
		SkillExternalID: skillID,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		log.Printf("list skill versions: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list skill versions"))
		return
	}
	var nextPage *string
	if hasMore {
		value := encodePageOffset(offset + len(versions))
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillVersionResponse]{
		Data:     responsesFromSkillVersions(versions),
		HasMore:  hasMore,
		NextPage: nextPage,
	})
}

func (h *Handler) retrieveVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieveVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) retrieveVersion(w http.ResponseWriter, r *http.Request, skillID, version string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before version retrieve: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill version"))
		return
	} else if ok {
		record, err := h.db.GetBuiltinSkillVersion(r.Context(), skillID, version)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
				return
			}
			log.Printf("get builtin skill version: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill version"))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, responseFromBuiltinVersion(record))
		return
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, version))
		return
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceID, skillID, version)
	if err != nil {
		writeResolveVersionError(w, r, skillID, version, err)
		return
	}
	record, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
			return
		}
		log.Printf("get skill version: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill version"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkillVersion(record))
}

func (h *Handler) deleteVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.deleteVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) deleteVersion(w http.ResponseWriter, r *http.Request, skillID, version string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before version delete: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete skill version"))
		return
	} else if ok {
		httpapi.WriteError(w, r, readOnlyBuiltinError())
		return
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": version, "type": "skill_version_deleted"})
		return
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceID, skillID, version)
	if err != nil {
		writeResolveVersionError(w, r, skillID, version, err)
		return
	}
	deletedVersion, _, err := h.db.SoftDeleteSkillVersion(r.Context(), principal.WorkspaceID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
			return
		}
		log.Printf("delete skill version: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete skill version"))
		return
	}
	h.deleteObjectOrEnqueueCleanup(r.Context(), deletedVersion)
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": resolved, "type": "skill_version_deleted"})
}

func (h *Handler) downloadVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.downloadVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) downloadVersion(w http.ResponseWriter, r *http.Request, skillID, version string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		log.Printf("get builtin skill before download: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download skill version"))
		return
	} else if ok {
		record, err := h.db.GetBuiltinSkillVersion(r.Context(), skillID, version)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
				return
			}
			log.Printf("get builtin skill version before download: %v", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download skill version"))
			return
		}
		h.downloadBuiltinSkill(w, r, record)
		return
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		h.downloadFixtureSkill(w, r)
		return
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceID, skillID, version)
	if err != nil {
		writeResolveVersionError(w, r, skillID, version, err)
		return
	}
	record, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
			return
		}
		log.Printf("get skill version before download: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download skill version"))
		return
	}
	object, err := h.store.Get(r.Context(), record.S3Key)
	if err != nil {
		log.Printf("get skill object skill_id=%s version=%s key=%s: %v", skillID, record.Version, record.S3Key, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download skill version"))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", skillArchiveContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.skill"`, sanitizeForHeader(record.Directory)))
	w.Header().Set("Content-Length", strconv.FormatInt(record.SizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		log.Printf("stream skill object skill_id=%s version=%s key=%s copied=%d expected=%d: %v", skillID, record.Version, record.S3Key, copied, record.SizeBytes, copyErr)
		return
	}
	if copied != record.SizeBytes {
		log.Printf("stream skill object size mismatch skill_id=%s version=%s key=%s copied=%d expected=%d", skillID, record.Version, record.S3Key, copied, record.SizeBytes)
	}
}

func (h *Handler) resolveVersion(ctx context.Context, workspaceID int64, skillID, version string) (string, error) {
	if version != "latest" {
		return version, nil
	}
	skill, err := h.db.GetSkill(ctx, workspaceID, skillID)
	if err != nil {
		return "", err
	}
	if skill.LatestVersion == nil || *skill.LatestVersion == "" {
		return "", db.ErrNotFound
	}
	return *skill.LatestVersion, nil
}

func (h *Handler) downloadBuiltinSkill(w http.ResponseWriter, r *http.Request, version db.BuiltinSkillVersion) {
	object, err := h.store.Get(r.Context(), version.S3Key)
	if err != nil {
		log.Printf("get builtin skill object skill_id=%s version=%s key=%s: %v", version.SkillExternalID, version.Version, version.S3Key, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download skill version"))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", skillArchiveContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.skill"`, sanitizeForHeader(version.Directory)))
	w.Header().Set("Content-Length", strconv.FormatInt(version.SizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		log.Printf("stream builtin skill skill_id=%s version=%s key=%s copied=%d expected=%d: %v", version.SkillExternalID, version.Version, version.S3Key, copied, version.SizeBytes, copyErr)
		return
	}
	if copied != version.SizeBytes {
		log.Printf("stream builtin skill size mismatch skill_id=%s version=%s key=%s copied=%d expected=%d", version.SkillExternalID, version.Version, version.S3Key, copied, version.SizeBytes)
	}
}

func (h *Handler) downloadFixtureSkill(w http.ResponseWriter, _ *http.Request) {
	data := fixtureArchive()
	w.Header().Set("Content-Type", skillArchiveContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="fixture-skill.skill"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) cleanupUploadedObjectAfterMetadataFailure(ctx context.Context, workspaceID int64, bucket, key, externalID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := h.store.Delete(cleanupCtx, key); err != nil {
		log.Printf("delete skill object after metadata failure key=%s: %v", key, err)
		if enqueueErr := h.db.EnqueueObjectCleanupJob(cleanupCtx, workspaceID, bucket, key, externalID); enqueueErr != nil {
			log.Printf("enqueue object cleanup key=%s: %v", key, enqueueErr)
		}
	}
}

func (h *Handler) deleteObjectOrEnqueueCleanup(ctx context.Context, version db.SkillVersion) {
	if err := h.store.Delete(ctx, version.S3Key); err != nil {
		log.Printf("delete skill object skill_id=%s version=%s key=%s: %v", version.SkillExternalID, version.Version, version.S3Key, err)
		if enqueueErr := h.db.EnqueueObjectCleanupJob(ctx, version.WorkspaceID, version.S3Bucket, version.S3Key, version.ExternalID); enqueueErr != nil {
			log.Printf("enqueue object cleanup skill_id=%s version=%s key=%s: %v", version.SkillExternalID, version.Version, version.S3Key, enqueueErr)
		}
	}
}

func responseFromSkill(skill db.Skill) skillResponse {
	return skillResponse{
		ID:            skill.ExternalID,
		CreatedAt:     formatTime(skill.CreatedAt),
		DisplayTitle:  valueOrEmpty(skill.DisplayTitle),
		LatestVersion: valueOrEmpty(skill.LatestVersion),
		Source:        skill.Source,
		Type:          "skill",
		UpdatedAt:     formatTime(skill.UpdatedAt),
	}
}

func responsesFromSkills(skills []db.Skill) []skillResponse {
	out := make([]skillResponse, 0, len(skills))
	for _, skill := range skills {
		out = append(out, responseFromSkill(skill))
	}
	return out
}

func responseFromBuiltinSkill(skill db.BuiltinSkill) skillResponse {
	return skillResponse{
		ID:            skill.ExternalID,
		CreatedAt:     formatTime(skill.CreatedAt),
		DisplayTitle:  firstNonEmpty(skill.DisplayTitle, skill.ExternalID),
		LatestVersion: valueOrEmpty(skill.LatestVersion),
		Source:        "anthropic",
		Type:          "skill",
		UpdatedAt:     formatTime(skill.UpdatedAt),
	}
}

func responsesFromBuiltinSkills(skills []db.BuiltinSkill) []skillResponse {
	out := make([]skillResponse, 0, len(skills))
	for _, skill := range skills {
		out = append(out, responseFromBuiltinSkill(skill))
	}
	return out
}

func responseFromSkillVersion(version db.SkillVersion) skillVersionResponse {
	return skillVersionResponse{
		ID:          version.ExternalID,
		CreatedAt:   formatTime(version.CreatedAt),
		Description: version.Description,
		Directory:   version.Directory,
		Name:        version.Name,
		SkillID:     version.SkillExternalID,
		Type:        "skill_version",
		Version:     version.Version,
	}
}

func responsesFromSkillVersions(versions []db.SkillVersion) []skillVersionResponse {
	out := make([]skillVersionResponse, 0, len(versions))
	for _, version := range versions {
		out = append(out, responseFromSkillVersion(version))
	}
	return out
}

func responseFromBuiltinVersion(version db.BuiltinSkillVersion) skillVersionResponse {
	return skillVersionResponse{
		ID:          version.ExternalID,
		CreatedAt:   formatTime(version.CreatedAt),
		Description: version.Description,
		Directory:   version.Directory,
		Name:        firstNonEmpty(version.Name, version.Directory),
		SkillID:     version.SkillExternalID,
		Type:        "skill_version",
		Version:     version.Version,
	}
}

func responsesFromBuiltinSkillVersions(versions []db.BuiltinSkillVersion) []skillVersionResponse {
	out := make([]skillVersionResponse, 0, len(versions))
	for _, version := range versions {
		out = append(out, responseFromBuiltinVersion(version))
	}
	return out
}

func (h *Handler) fixtureSkillResponse(skillID, displayTitle string) skillResponse {
	now := time.Unix(0, 0).UTC()
	return skillResponse{
		ID:            skillID,
		CreatedAt:     formatTime(now),
		DisplayTitle:  firstNonEmpty(displayTitle, "display_title"),
		LatestVersion: h.cfg.OfficialSDKFixtureSkillVersion,
		Source:        "custom",
		Type:          "skill",
		UpdatedAt:     formatTime(now),
	}
}

func (h *Handler) fixtureVersionResponse(skillID, version string) skillVersionResponse {
	return skillVersionResponse{
		ID:          "skillver_fixture",
		CreatedAt:   formatTime(time.Unix(0, 0).UTC()),
		Description: "description",
		Directory:   "fixture-skill",
		Name:        "fixture-skill",
		SkillID:     skillID,
		Type:        "skill_version",
		Version:     version,
	}
}

func fixtureArchive() []byte {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("fixture-skill/SKILL.md")
	if err == nil {
		_, _ = entry.Write([]byte("---\nname: fixture-skill\ndescription: description\n---\n\n# fixture-skill\n"))
	}
	_ = writer.Close()
	return buf.Bytes()
}

func parseLimitParam(r *http.Request, defaultLimit, maxLimit int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 0 {
		return 0, errors.New("limit must be at least 0")
	}
	if limit > maxLimit {
		return 0, fmt.Errorf("limit must be at most %d", maxLimit)
	}
	return limit, nil
}

func decodePageOffset(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, errors.New("page is invalid")
	}
	var cursor pageCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return 0, errors.New("page is invalid")
	}
	if cursor.Offset < 0 {
		return 0, errors.New("page is invalid")
	}
	return cursor.Offset, nil
}

func encodePageOffset(offset int) string {
	data, _ := json.Marshal(pageCursor{Offset: offset})
	return base64.RawURLEncoding.EncodeToString(data)
}

func hasSkillsBeta(r *http.Request) bool {
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == skillsBeta {
				return true
			}
		}
	}
	return false
}

func requireWorkspaceCredential(principal auth.Principal) *httpapi.Error {
	if principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession {
		return nil
	}
	return httpapi.NewError(http.StatusForbidden, "permission_error", "Credential cannot access skills")
}

func readOnlyBuiltinError() *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Built-in skills are read-only")
}

func writePackageError(w http.ResponseWriter, r *http.Request, err error) {
	var packageErr packageError
	if errors.As(err, &packageErr) {
		httpapi.WriteError(w, r, httpapi.NewError(packageErr.Status, "invalid_request_error", packageErr.Message))
		return
	}
	log.Printf("read skill package: %v", err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not read skill package"))
}

func writeResolveVersionError(w http.ResponseWriter, r *http.Request, skillID, version string, err error) {
	if errors.Is(err, db.ErrNotFound) {
		if version == "latest" {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill not found: "+skillID))
			return
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Skill version not found: "+version))
		return
	}
	log.Printf("resolve skill version skill_id=%s version=%s: %v", skillID, version, err)
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve skill version"))
}

func (h *Handler) isOfficialSDKFixturePrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" && principal.APIKeyExternalID == h.cfg.OfficialSDKResourceAPIKeyExternalID
}

func (h *Handler) isOfficialSDKFixtureSkill(principal auth.Principal, skillID string) bool {
	return h.isOfficialSDKFixturePrincipal(principal) && skillID == h.cfg.OfficialSDKFixtureSkillID
}

func (h *Handler) isOfficialSDKFixtureVersion(principal auth.Principal, skillID, version string) bool {
	return h.isOfficialSDKFixtureSkill(principal, skillID) && (version == h.cfg.OfficialSDKFixtureSkillVersion || version == "latest")
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func newVersionString() string {
	return strconv.FormatInt(time.Now().UTC().UnixMicro(), 10)
}

func sanitizeForKey(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" {
		return "skill"
	}
	if !utf8.ValidString(name) {
		return "skill"
	}
	var builder strings.Builder
	for _, r := range name {
		if r < 32 || r == 127 {
			builder.WriteByte('_')
			continue
		}
		builder.WriteRune(r)
	}
	value := strings.Trim(builder.String(), ". ")
	if value == "" {
		return "skill"
	}
	return value
}

func sanitizeForHeader(name string) string {
	name = sanitizeForKey(name)
	return strings.ReplaceAll(name, `"`, "_")
}
