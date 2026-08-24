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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
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
	cfg          config.Config
	db           *db.DB
	logger       *slog.Logger
	store        storage.ObjectStore
	errorAdapter *httpapi.ErrorAdapter
	router       chi.Router
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

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	h := &Handler{
		cfg:          cfg,
		db:           database,
		logger:       logger,
		store:        store,
		errorAdapter: httpapi.NewErrorAdapter(logger),
	}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Get("/{skill_id}", wrap(h.retrieveRoute))
	router.Delete("/{skill_id}", wrap(h.deleteRoute))
	router.Post("/{skill_id}/versions", wrap(h.createVersionRoute))
	router.Get("/{skill_id}/versions", wrap(h.listVersionsRoute))
	router.Get("/{skill_id}/versions/{version}", wrap(h.retrieveVersionRoute))
	router.Delete("/{skill_id}/versions/{version}", wrap(h.deleteVersionRoute))
	router.Get("/{skill_id}/versions/{version}/content", h.downloadVersionRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" || !hasSkillsBeta(r) {
		h.errorAdapter.Write(w, r, skillsBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return skillRouteNotFound()
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return skillAuthenticationRequired()
	}
	if err := requireWorkspaceCredential(principal); err != nil {
		return err
	}

	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		if h.isOfficialSDKFixturePrincipal(principal) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureSkillResponse(h.cfg.SDKFixtures.SkillID, firstNonEmpty(r.FormValue("display_title"), "display_title")))
			return nil
		}
		return mapSkillPackageError(err)
	}

	skillID, err := ids.New("skill_")
	if err != nil {
		return internalError("Could not generate skill ID", fmt.Errorf("generate skill ID: %w", err))
	}
	versionID, err := ids.New("skillver_")
	if err != nil {
		return internalError("Could not generate skill version ID", fmt.Errorf("generate skill version ID: %w", err))
	}
	skillUUID := uuid.NewV4().String()
	versionUUID := uuid.NewV4().String()
	versionValue := newVersionString()
	objectKey := fmt.Sprintf("workspaces/%s/skills/%s/versions/%s/%s.zip", principal.WorkspaceUUID, skillUUID, versionValue, sanitizeForKey(pkg.Directory))

	if _, err := h.store.Upload(r.Context(), objectKey, bytes.NewReader(pkg.Zip), storage.UploadOptions{Size: pkg.Size, ContentType: skillArchiveContentType}); err != nil {
		return internalError("Could not store skill", fmt.Errorf("store skill %q archive: %w", skillID, err))
	}

	now := time.Now().UTC()
	displayTitle := firstNonEmpty(r.FormValue("display_title"), pkg.Name, pkg.Directory)
	createdSkill, _, err := h.db.CreateSkillWithVersion(r.Context(), db.Skill{
		UUID:                skillUUID,
		ExternalID:          skillID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		DisplayTitle:        &displayTitle,
		CreatedAt:           now,
	}, db.SkillVersion{
		UUID:                versionUUID,
		ExternalID:          versionID,
		WorkspaceUUID:       principal.WorkspaceUUID,
		Version:             versionValue,
		Name:                pkg.Name,
		Description:         pkg.Description,
		Directory:           pkg.Directory,
		S3Bucket:            h.store.Name(),
		S3Key:               objectKey,
		SizeBytes:           pkg.Size,
		SHA256:              pkg.SHA256,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		CreatedAt:           now,
	})
	if err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), principal.WorkspaceUUID, h.store.Name(), objectKey, versionID)
		var displayTitleConflict *db.SkillDisplayTitleConflictError
		if errors.As(err, &displayTitleConflict) {
			return skillDisplayTitleConflict(displayTitleConflict.DisplayTitle, err)
		}
		return internalError("Could not create skill", fmt.Errorf("create skill %q metadata: %w", skillID, err))
	}

	httpapi.WriteJSON(w, http.StatusOK, responseFromSkill(createdSkill))
	return nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	if source != "" && source != "custom" && source != "anthropic" {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillResponse]{Data: []skillResponse{}, HasMore: false, NextPage: nil})
		return nil
	}
	limit, err := parseLimitParam(r, defaultSkillsLimit, maxSkillsLimit)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
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
			return internalError("Could not list skills", fmt.Errorf("list built-in skills: %w", err))
		}
		data = responsesFromBuiltinSkills(builtins)
		hasMore = more
	case "custom":
		records, more, err := h.db.ListSkillsPage(r.Context(), db.ListSkillsPageParams{
			WorkspaceUUID: principal.WorkspaceUUID,
			Limit:         limit,
			Offset:        offset,
		})
		if err != nil {
			return internalError("Could not list skills", fmt.Errorf("list custom skills: %w", err))
		}
		data = responsesFromSkills(records)
		hasMore = more
	default:
		data, hasMore, err = h.listAllSkills(r, principal, offset, limit)
		if err != nil {
			return internalError("Could not list skills", fmt.Errorf("list all skills: %w", err))
		}
	}

	var nextPage *string
	if hasMore {
		value := encodePageOffset(offset + len(data))
		nextPage = &value
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillResponse]{Data: data, HasMore: hasMore, NextPage: nextPage})
	return nil
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
					WorkspaceUUID: principal.WorkspaceUUID,
					Limit:         1,
					Offset:        0,
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
			WorkspaceUUID: principal.WorkspaceUUID,
			Limit:         customLimit,
			Offset:        0,
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
		WorkspaceUUID: principal.WorkspaceUUID,
		Limit:         limit,
		Offset:        offset - builtinCount,
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

func (h *Handler) retrieveRoute(w http.ResponseWriter, r *http.Request) error {
	return h.retrieve(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request, skillID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if skill, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not retrieve skill", fmt.Errorf("retrieve built-in skill %q: %w", skillID, err))
	} else if ok {
		httpapi.WriteJSON(w, http.StatusOK, responseFromBuiltinSkill(skill))
		return nil
	}
	record, err := h.db.GetSkill(r.Context(), principal.WorkspaceUUID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureSkillResponse(skillID, "display_title"))
			return nil
		}
		if errors.Is(err, db.ErrNotFound) {
			return skillNotFound(skillID, err)
		}
		return internalError("Could not retrieve skill", fmt.Errorf("retrieve skill %q: %w", skillID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkill(record))
	return nil
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) error {
	return h.delete(w, r, chi.URLParam(r, "skill_id"))
}

// TODO: 将 custom skill/version 软删除产生的 archive 标记为 catalog GC candidate，
// 并仅在不存在活动 Skill Archive Resource 引用时由后台任务删除对象。
// 当前必须保留 archive，以保证已经启动的 Session 仍能读取钉住的具体版本。
func (h *Handler) delete(w http.ResponseWriter, r *http.Request, skillID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		return err
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not delete skill", fmt.Errorf("retrieve built-in skill %q before delete: %w", skillID, err))
	} else if ok {
		return readOnlyBuiltinError()
	}

	_, _, err := h.db.SoftDeleteSkill(r.Context(), principal.WorkspaceUUID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": skillID, "type": "skill_deleted"})
			return nil
		}
		if errors.Is(err, db.ErrNotFound) {
			return skillNotFound(skillID, err)
		}
		return internalError("Could not delete skill", fmt.Errorf("delete skill %q: %w", skillID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": skillID, "type": "skill_deleted"})
	return nil
}

func (h *Handler) createVersionRoute(w http.ResponseWriter, r *http.Request) error {
	return h.createVersion(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) createVersion(w http.ResponseWriter, r *http.Request, skillID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		return err
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not create skill version", fmt.Errorf("retrieve built-in skill %q before version create: %w", skillID, err))
	} else if ok {
		return readOnlyBuiltinError()
	}

	pkg, err := readSkillPackage(w, r, MaxSkillPackageBytes)
	if err != nil {
		if h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, h.cfg.SDKFixtures.SkillVersion))
			return nil
		}
		return mapSkillPackageError(err)
	}

	skill, err := h.db.GetSkill(r.Context(), principal.WorkspaceUUID, skillID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixtureSkill(principal, skillID) {
			httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, h.cfg.SDKFixtures.SkillVersion))
			return nil
		}
		if errors.Is(err, db.ErrNotFound) {
			return skillNotFound(skillID, err)
		}
		return internalError("Could not create skill version", fmt.Errorf("retrieve skill %q before version create: %w", skillID, err))
	}

	versionID, err := ids.New("skillver_")
	if err != nil {
		return internalError("Could not generate skill version ID", fmt.Errorf("generate skill version ID: %w", err))
	}
	versionUUID := uuid.NewV4().String()
	versionValue := newVersionString()
	objectKey := fmt.Sprintf("workspaces/%s/skills/%s/versions/%s/%s.zip", principal.WorkspaceUUID, skill.UUID, versionValue, sanitizeForKey(pkg.Directory))
	if _, err := h.store.Upload(r.Context(), objectKey, bytes.NewReader(pkg.Zip), storage.UploadOptions{Size: pkg.Size, ContentType: skillArchiveContentType}); err != nil {
		return internalError("Could not store skill version", fmt.Errorf("store skill %q version archive: %w", skillID, err))
	}

	now := time.Now().UTC()
	_, version, err := h.db.CreateSkillVersion(r.Context(), principal.WorkspaceUUID, skillID, db.SkillVersion{
		UUID:                versionUUID,
		ExternalID:          versionID,
		Version:             versionValue,
		Name:                pkg.Name,
		Description:         pkg.Description,
		Directory:           pkg.Directory,
		S3Bucket:            h.store.Name(),
		S3Key:               objectKey,
		SizeBytes:           pkg.Size,
		SHA256:              pkg.SHA256,
		CreatedByAPIKeyUUID: principal.APIKeyUUID,
		CreatedAt:           now,
	})
	if err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), principal.WorkspaceUUID, h.store.Name(), objectKey, versionID)
		if errors.Is(err, db.ErrNotFound) {
			return skillNotFound(skillID, err)
		}
		return internalError("Could not create skill version", fmt.Errorf("create skill %q version metadata: %w", skillID, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkillVersion(version))
	return nil
}

func (h *Handler) listVersionsRoute(w http.ResponseWriter, r *http.Request) error {
	return h.listVersions(w, r, chi.URLParam(r, "skill_id"))
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request, skillID string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not list skill versions", fmt.Errorf("retrieve built-in skill %q before version list: %w", skillID, err))
	} else if ok {
		limit, err := parseLimitParam(r, defaultSkillVersionsLimit, maxSkillVersionsLimit)
		if err != nil {
			return invalidRequest(err)
		}
		offset, err := decodePageOffset(r.URL.Query().Get("page"))
		if err != nil {
			return invalidRequest(err)
		}
		versions, hasMore, err := h.db.ListBuiltinSkillVersionsPage(r.Context(), db.ListBuiltinSkillVersionsPageParams{
			SkillExternalID: skillID,
			Limit:           limit,
			Offset:          offset,
		})
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return skillNotFound(skillID, err)
			}
			return internalError("Could not list skill versions", fmt.Errorf("list built-in skill %q versions: %w", skillID, err))
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
		return nil
	}
	if h.isOfficialSDKFixtureSkill(principal, skillID) {
		httpapi.WriteJSON(w, http.StatusOK, pageResponse[skillVersionResponse]{
			Data:     []skillVersionResponse{h.fixtureVersionResponse(skillID, h.cfg.SDKFixtures.SkillVersion)},
			HasMore:  false,
			NextPage: nil,
		})
		return nil
	}

	limit, err := parseLimitParam(r, defaultSkillVersionsLimit, maxSkillVersionsLimit)
	if err != nil {
		return invalidRequest(err)
	}
	offset, err := decodePageOffset(r.URL.Query().Get("page"))
	if err != nil {
		return invalidRequest(err)
	}
	versions, hasMore, err := h.db.ListSkillVersionsPage(r.Context(), db.ListSkillVersionsPageParams{
		WorkspaceUUID:   principal.WorkspaceUUID,
		SkillExternalID: skillID,
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return skillNotFound(skillID, err)
		}
		return internalError("Could not list skill versions", fmt.Errorf("list skill %q versions: %w", skillID, err))
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
	return nil
}

func (h *Handler) retrieveVersionRoute(w http.ResponseWriter, r *http.Request) error {
	return h.retrieveVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) retrieveVersion(w http.ResponseWriter, r *http.Request, skillID, version string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not retrieve skill version", fmt.Errorf("retrieve built-in skill %q before version retrieve: %w", skillID, err))
	} else if ok {
		record, err := h.db.GetBuiltinSkillVersion(r.Context(), skillID, version)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return skillVersionNotFound(version, err)
			}
			return internalError("Could not retrieve skill version", fmt.Errorf("retrieve built-in skill %q version %q: %w", skillID, version, err))
		}
		httpapi.WriteJSON(w, http.StatusOK, responseFromBuiltinVersion(record))
		return nil
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		httpapi.WriteJSON(w, http.StatusOK, h.fixtureVersionResponse(skillID, version))
		return nil
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceUUID, skillID, version)
	if err != nil {
		return mapResolveVersionError(skillID, version, err)
	}
	record, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceUUID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return skillVersionNotFound(version, err)
		}
		return internalError("Could not retrieve skill version", fmt.Errorf("retrieve skill %q version %q: %w", skillID, resolved, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, responseFromSkillVersion(record))
	return nil
}

func (h *Handler) deleteVersionRoute(w http.ResponseWriter, r *http.Request) error {
	return h.deleteVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) deleteVersion(w http.ResponseWriter, r *http.Request, skillID, version string) error {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := requireWorkspaceCredential(principal); err != nil {
		return err
	}
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		return internalError("Could not delete skill version", fmt.Errorf("retrieve built-in skill %q before version delete: %w", skillID, err))
	} else if ok {
		return readOnlyBuiltinError()
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": version, "type": "skill_version_deleted"})
		return nil
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceUUID, skillID, version)
	if err != nil {
		return mapResolveVersionError(skillID, version, err)
	}
	_, _, err = h.db.SoftDeleteSkillVersion(r.Context(), principal.WorkspaceUUID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return skillVersionNotFound(version, err)
		}
		return internalError("Could not delete skill version", fmt.Errorf("delete skill %q version %q: %w", skillID, resolved, err))
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": resolved, "type": "skill_version_deleted"})
	return nil
}

func (h *Handler) downloadVersionRoute(w http.ResponseWriter, r *http.Request) {
	h.downloadVersion(w, r, chi.URLParam(r, "skill_id"), chi.URLParam(r, "version"))
}

func (h *Handler) downloadVersion(w http.ResponseWriter, r *http.Request, skillID, version string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	if _, ok, err := h.getBuiltinSkill(r.Context(), skillID); err != nil {
		h.errorAdapter.Write(w, r, skillDownloadError(fmt.Errorf("get builtin skill %q before download: %w", skillID, err)))
		return
	} else if ok {
		record, err := h.db.GetBuiltinSkillVersion(r.Context(), skillID, version)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				h.errorAdapter.Write(w, r, skillVersionNotFound(version, err))
				return
			}
			h.errorAdapter.Write(w, r, skillDownloadError(fmt.Errorf("get builtin skill %q version %q before download: %w", skillID, version, err)))
			return
		}
		h.downloadBuiltinSkill(w, r, record)
		return
	}
	if h.isOfficialSDKFixtureVersion(principal, skillID, version) {
		h.downloadFixtureSkill(w, r)
		return
	}

	resolved, err := h.resolveVersion(r.Context(), principal.WorkspaceUUID, skillID, version)
	if err != nil {
		h.errorAdapter.Write(w, r, mapResolveVersionError(skillID, version, err))
		return
	}
	record, err := h.db.GetSkillVersion(r.Context(), principal.WorkspaceUUID, skillID, resolved)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			h.errorAdapter.Write(w, r, skillVersionNotFound(version, err))
			return
		}
		h.errorAdapter.Write(w, r, skillDownloadError(fmt.Errorf("get skill %q version %q before download: %w", skillID, resolved, err)))
		return
	}
	object, err := h.store.Open(r.Context(), record.S3Key, nil)
	if err != nil {
		h.errorAdapter.Write(w, r, skillDownloadError(fmt.Errorf("open skill %q version %q object %q: %w", skillID, record.Version, record.S3Key, err)))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", skillArchiveContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.skill"`, sanitizeForHeader(record.Directory)))
	w.Header().Set("Content-Length", strconv.FormatInt(record.SizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		h.logger.ErrorContext(r.Context(), "stream skill object", "skill_id", skillID, "version", record.Version, "key", record.S3Key, "bytes_copied", copied, "expected_size", record.SizeBytes, "error", copyErr)
		return
	}
	if copied != record.SizeBytes {
		h.logger.WarnContext(r.Context(), "stream skill object size mismatch", "skill_id", skillID, "version", record.Version, "key", record.S3Key, "bytes_copied", copied, "expected_size", record.SizeBytes)
	}
}

func (h *Handler) resolveVersion(ctx context.Context, workspaceUUID string, skillID, version string) (string, error) {
	if version != "latest" {
		return version, nil
	}
	skill, err := h.db.GetSkill(ctx, workspaceUUID, skillID)
	if err != nil {
		return "", err
	}
	if skill.LatestVersion == nil || *skill.LatestVersion == "" {
		return "", db.ErrNotFound
	}
	return *skill.LatestVersion, nil
}

func (h *Handler) downloadBuiltinSkill(w http.ResponseWriter, r *http.Request, version db.BuiltinSkillVersion) {
	object, err := h.store.Open(r.Context(), version.S3Key, nil)
	if err != nil {
		h.errorAdapter.Write(w, r, skillDownloadError(fmt.Errorf("open builtin skill %q version %q object %q: %w", version.SkillExternalID, version.Version, version.S3Key, err)))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", skillArchiveContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.skill"`, sanitizeForHeader(version.Directory)))
	w.Header().Set("Content-Length", strconv.FormatInt(version.SizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		h.logger.ErrorContext(r.Context(), "stream builtin skill", "skill_id", version.SkillExternalID, "version", version.Version, "key", version.S3Key, "bytes_copied", copied, "expected_size", version.SizeBytes, "error", copyErr)
		return
	}
	if copied != version.SizeBytes {
		h.logger.WarnContext(r.Context(), "stream builtin skill size mismatch", "skill_id", version.SkillExternalID, "version", version.Version, "key", version.S3Key, "bytes_copied", copied, "expected_size", version.SizeBytes)
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

func (h *Handler) cleanupUploadedObjectAfterMetadataFailure(ctx context.Context, workspaceUUID string, bucket, key, externalID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := h.store.Delete(cleanupCtx, key, storage.DeleteOptions{}); err != nil {
		h.logger.ErrorContext(ctx, "delete skill object after metadata failure", "key", key, "error", err)
		if enqueueErr := h.db.EnqueueObjectCleanupJob(cleanupCtx, workspaceUUID, bucket, key, externalID); enqueueErr != nil {
			h.logger.ErrorContext(ctx, "enqueue object cleanup", "key", key, "error", enqueueErr)
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
		LatestVersion: h.cfg.SDKFixtures.SkillVersion,
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

func (h *Handler) isOfficialSDKFixturePrincipal(principal auth.Principal) bool {
	return principal.CredentialType == "api_key" && principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) isOfficialSDKFixtureSkill(principal auth.Principal, skillID string) bool {
	return h.isOfficialSDKFixturePrincipal(principal) && skillID == h.cfg.SDKFixtures.SkillID
}

func (h *Handler) isOfficialSDKFixtureVersion(principal auth.Principal, skillID, version string) bool {
	return h.isOfficialSDKFixtureSkill(principal, skillID) && (version == h.cfg.SDKFixtures.SkillVersion || version == "latest")
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
