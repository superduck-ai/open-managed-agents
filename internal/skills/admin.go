package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type systemSkillResponse struct {
	ID             string    `json:"id"`
	DisplayTitle   string    `json:"display_title"`
	IconKey        string    `json:"icon_key"`
	DefaultVersion *string   `json:"default_version"`
	Listed         bool      `json:"listed"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type systemVersionResponse struct {
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

type systemEventResponse struct {
	ID        string    `json:"id"`
	ActorUUID string    `json:"actor_uuid"`
	Action    string    `json:"action"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

type systemPackageResponse struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	SHA256      string               `json:"sha256"`
	SizeBytes   int64                `json:"size_bytes"`
	Files       []string             `json:"files"`
	SkillMD     string               `json:"skill_md"`
	Existing    *systemSkillResponse `json:"existing"`
}

type systemPage[T any] struct {
	Data    []T  `json:"data"`
	HasMore bool `json:"has_more"`
}

type systemSkillAction struct {
	Revision int64  `json:"revision"`
	Action   string `json:"action"`
	Version  string `json:"version"`
}

func (h *Handler) AdminRoutes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.requireSystemAdministrator)
	r.Get("/", h.systemWrap(h.listSystemSkills))
	r.Post("/inspect", h.systemWrap(h.inspectSystemPackage))
	r.Post("/", h.systemWrap(h.importSystemPackage))
	r.Get("/{skill_id}", h.systemWrap(h.getSystemSkill))
	r.Patch("/{skill_id}/icon", h.systemWrap(h.changeSystemIcon))
	r.Post("/{skill_id}/actions", h.systemWrap(h.changeSystemSkill))
	r.Get("/{skill_id}/versions", h.systemWrap(h.systemVersions))
	r.Get("/{skill_id}/events", h.systemWrap(h.systemEvents))
	r.Get("/{skill_id}/versions/{version}/content", h.systemWrap(h.systemContent))
	return r
}

func (h *Handler) requireSystemAdministrator(next http.Handler) http.Handler {
	return h.systemWrap(func(w http.ResponseWriter, r *http.Request) error {
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok || principal.CredentialType != auth.CredentialTypePlatformSession || principal.UserUUID == "" {
			return errSystemSkillDenied
		}
		next.ServeHTTP(w, r)
		return nil
	})
}

func (h *Handler) systemWrap(next httpapi.Endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := next(w, r); err != nil {
			mapped := systemSkillError(err)
			if mapped.Status >= 500 {
				h.logger.ErrorContext(r.Context(), "system skill administration failed", "request_id", httpapi.RequestID(r.Context()), "error", err)
			}
			httpapi.WriteJSON(w, mapped.Status, struct {
				Error     *systemSkillPublicError `json:"error"`
				RequestID string                  `json:"request_id"`
			}{mapped, httpapi.RequestID(r.Context())})
		}
	}
}

func systemSkillDTO(s db.SystemSkill) systemSkillResponse {
	return systemSkillResponse{ID: s.ExternalID, DisplayTitle: s.DisplayTitle, IconKey: s.IconKey, DefaultVersion: s.LatestVersion, Listed: s.Listed, Revision: s.Revision, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

func systemPaging(r *http.Request) (int, int, error) {
	limit, offset := 20, 0
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, systemInvalid("分页数量必须为 1–100")
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil || offset < 0 || offset > 100000 {
			return 0, 0, systemInvalid("分页位置无效")
		}
	}
	return limit, offset, nil
}

func (h *Handler) listSystemSkills(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := systemPaging(r)
	if err != nil {
		return err
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "listed" && status != "unlisted" && status != "draft" {
		return systemInvalid("技能状态无效")
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if len(query) > 256 {
		return systemInvalid("搜索内容过长")
	}
	records, err := h.db.ListSystemSkills(r.Context(), db.SystemSkillQuery{Query: query, Status: status, Limit: limit + 1, Offset: offset})
	if err != nil {
		return err
	}
	more := len(records) > limit
	records = records[:min(len(records), limit)]
	data := make([]systemSkillResponse, 0, len(records))
	for _, record := range records {
		data = append(data, systemSkillDTO(record))
	}
	httpapi.WriteJSON(w, 200, systemPage[systemSkillResponse]{Data: data, HasMore: more})
	return nil
}

func (h *Handler) getSystemSkill(w http.ResponseWriter, r *http.Request) error {
	skill, err := h.db.GetSystemSkill(r.Context(), chi.URLParam(r, "skill_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, systemSkillDTO(skill))
	return nil
}

func (h *Handler) systemVersions(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := systemPaging(r)
	if err != nil {
		return err
	}
	skill, err := h.db.GetSystemSkill(r.Context(), chi.URLParam(r, "skill_id"))
	if err != nil {
		return err
	}
	records, err := h.db.SystemSkillVersions(r.Context(), skill.UUID, limit+1, offset)
	if err != nil {
		return err
	}
	more := len(records) > limit
	records = records[:min(len(records), limit)]
	data := make([]systemVersionResponse, 0, len(records))
	for _, v := range records {
		data = append(data, systemVersionResponse{ID: v.ExternalID, Version: v.Version, Name: v.Name, Description: v.Description, State: v.State, SizeBytes: v.SizeBytes, SHA256: v.SHA256, CreatedAt: v.CreatedAt})
	}
	httpapi.WriteJSON(w, 200, systemPage[systemVersionResponse]{Data: data, HasMore: more})
	return nil
}

func (h *Handler) systemEvents(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := systemPaging(r)
	if err != nil {
		return err
	}
	skill, err := h.db.GetSystemSkill(r.Context(), chi.URLParam(r, "skill_id"))
	if err != nil {
		return err
	}
	records, err := h.db.SystemSkillEvents(r.Context(), skill.UUID, limit+1, offset)
	if err != nil {
		return err
	}
	more := len(records) > limit
	records = records[:min(len(records), limit)]
	data := make([]systemEventResponse, 0, len(records))
	for _, v := range records {
		data = append(data, systemEventResponse{ID: v.UUID, ActorUUID: v.ActorUUID, Action: v.Action, Version: v.Version, CreatedAt: v.CreatedAt})
	}
	httpapi.WriteJSON(w, 200, systemPage[systemEventResponse]{Data: data, HasMore: more})
	return nil
}

func (h *Handler) changeSystemSkill(w http.ResponseWriter, r *http.Request) error {
	var input systemSkillAction
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 8192), &input, json.RejectUnknownMembers(true)); err != nil {
		return systemInvalid("请求字段或 JSON 格式无效")
	}
	if input.Revision < 1 || !validSystemVersion(input.Version, true) {
		return systemInvalid("版本或修订号无效")
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	var result db.SystemSkill
	err := h.withSystemSkillWrite(r.Context(), principal.UserUUID, func(tx *db.PlatformAdminTx) error {
		var err error
		result, err = tx.ChangeSystemSkill(r.Context(), db.SystemSkillChange{ActorUUID: principal.UserUUID, SkillID: chi.URLParam(r, "skill_id"), Revision: input.Revision, Action: input.Action, Version: input.Version})
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, systemSkillDTO(result))
	return nil
}

func (h *Handler) inspectSystemPackage(w http.ResponseWriter, r *http.Request) error {
	pkg, err := readSkillPackageOptions(w, r, MaxSkillPackageBytes, true)
	if err != nil {
		return err
	}
	result, err := systemPackageDTO(pkg)
	if err != nil {
		return err
	}
	id := pkg.Name
	if target := strings.TrimSpace(r.FormValue("skill_id")); target != "" {
		id = target
	}
	skill, err := h.db.GetSystemSkill(r.Context(), id)
	if err == nil {
		result.Existing = new(systemSkillDTO(skill))
	} else if !errors.Is(err, db.ErrNotFound) {
		return err
	}
	httpapi.WriteJSON(w, 200, result)
	return nil
}

func systemPackageDTO(pkg skillPackage) (systemPackageResponse, error) {
	reader, err := zip.NewReader(bytes.NewReader(pkg.Zip), int64(len(pkg.Zip)))
	if err != nil {
		return systemPackageResponse{}, err
	}
	result := systemPackageResponse{Name: pkg.Name, Description: pkg.Description, SHA256: pkg.SHA256, SizeBytes: pkg.Size, Files: make([]string, 0, len(reader.File))}
	for _, file := range reader.File {
		result.Files = append(result.Files, file.Name)
		if file.Name == pkg.Directory+"/SKILL.md" {
			data, err := readZipFile(file, 256*1024)
			if budget, ok := errors.AsType[packageError](err); ok && budget.Status == http.StatusRequestEntityTooLarge {
				return result, systemSkillPreviewTooLarge()
			}
			if err != nil {
				return result, err
			}
			result.SkillMD = string(data)
		}
	}
	return result, nil
}

func (h *Handler) systemContent(w http.ResponseWriter, r *http.Request) error {
	skill, err := h.db.GetSystemSkill(r.Context(), chi.URLParam(r, "skill_id"))
	if err != nil {
		return err
	}
	version, err := h.db.SystemSkillVersion(r.Context(), skill.UUID, chi.URLParam(r, "version"))
	if err != nil {
		return err
	}
	result, err := h.packagePreview(r.Context(), version.S3Bucket, version.S3Key, skillPackage{Name: version.Name, Description: version.Description, Directory: version.Directory, Size: version.SizeBytes, SHA256: version.SHA256}, MaxSkillPackageBytes)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, result)
	return nil
}

func (h *Handler) packagePreview(ctx context.Context, bucket, key string, pkg skillPackage, limit int64) (systemPackageResponse, error) {
	if bucket != h.store.Name() {
		return systemPackageResponse{}, systemInvalid("技能存储配置不匹配")
	}
	object, err := h.store.Open(ctx, key, nil)
	if err != nil {
		return systemPackageResponse{}, err
	}
	defer object.Body.Close()
	archive, err := readWithinBudget(object.Body, limit)
	if err != nil {
		return systemPackageResponse{}, err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != pkg.SHA256 {
		return systemPackageResponse{}, systemInvalid("技能包完整性检查失败")
	}
	pkg.Zip = archive
	return systemPackageDTO(pkg)
}

func (h *Handler) importSystemPackage(w http.ResponseWriter, r *http.Request) error {
	pkg, err := readSkillPackageOptions(w, r, MaxSkillPackageBytes, true)
	if err != nil {
		return err
	}
	if _, err := systemPackageDTO(pkg); err != nil {
		return err
	}
	input, err := systemImportInput(r, pkg)
	if err != nil {
		return err
	}
	versionID, err := ids.New("skver_")
	if err != nil {
		return err
	}
	input.Version.ExternalID = versionID
	input.Version.S3Bucket = h.store.Name()
	input.Version.S3Key = "builtin-skills/uploads/" + input.Version.UUID + ".zip"
	if _, err := h.store.Upload(r.Context(), input.Version.S3Key, bytes.NewReader(pkg.Zip), storage.UploadOptions{Size: pkg.Size, ContentType: skillArchiveContentType}); err != nil {
		return err
	}
	var skill db.SystemSkill
	duplicate := false
	err = h.withSystemSkillWrite(r.Context(), input.ActorUUID, func(tx *db.PlatformAdminTx) error {
		var err error
		skill, duplicate, err = tx.ImportSystemSkill(r.Context(), input)
		return err
	})
	if err != nil || duplicate {
		h.cleanupSystemUpload(r.Context(), input.Version.S3Key)
	}
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, struct {
		Skill     systemSkillResponse `json:"skill"`
		Duplicate bool                `json:"duplicate"`
	}{systemSkillDTO(skill), duplicate})
	return nil
}

func (h *Handler) cleanupSystemUpload(ctx context.Context, key string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	referenced, err := h.db.HasSystemSkillArchive(cleanup, key)
	if err != nil {
		h.logger.ErrorContext(ctx, "retain system skill upload after uncertain commit", "object_key", key, "error", err)
		return
	}
	if referenced {
		return
	}
	if err := h.store.Delete(cleanup, key, storage.DeleteOptions{}); err != nil {
		h.logger.ErrorContext(ctx, "system skill upload cleanup failed", "object_key", key, "error", err)
	}
}

func systemImportInput(r *http.Request, pkg skillPackage) (db.SystemSkillImport, error) {
	version := strings.TrimSpace(r.FormValue("version"))
	automatic := version == ""
	if automatic {
		version = "auto-" + uuid.New().String()
	}
	if !validSystemVersion(version, false) {
		return db.SystemSkillImport{}, systemInvalid("请填写有效版本标识（最多 64 个字母、数字、点、下划线或横线）")
	}
	revision := int64(0)
	if value := r.FormValue("revision"); value != "" {
		var err error
		revision, err = strconv.ParseInt(value, 10, 64)
		if err != nil || revision < 0 {
			return db.SystemSkillImport{}, systemInvalid("修订号无效")
		}
	}
	target := strings.TrimSpace(r.FormValue("skill_id"))
	skillID := pkg.Name
	if target != "" {
		if revision < 1 || len(target) > 256 {
			return db.SystemSkillImport{}, systemInvalid("目标技能或修订号无效")
		}
		skillID = target
	}
	title := strings.TrimSpace(r.FormValue("display_title"))
	if title == "" {
		title = pkg.Name
	}
	if len(title) > 256 {
		return db.SystemSkillImport{}, systemInvalid("技能说明字段过长")
	}
	publish := r.FormValue("publish")
	if !automatic && publish != "true" && publish != "false" {
		return db.SystemSkillImport{}, systemInvalid("请选择发布或保存草稿")
	}
	iconKey, err := skillIconKey(r.FormValue("icon_key"))
	if err != nil {
		return db.SystemSkillImport{}, err
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	return db.SystemSkillImport{ActorUUID: principal.UserUUID, ExpectedRevision: revision, Publish: publish == "true", Ready: automatic, Skill: db.SystemSkill{UUID: uuid.NewV4().String(), ExternalID: skillID, DisplayTitle: title, IconKey: iconKey}, Version: db.SystemSkillVersion{UUID: uuid.NewV4().String(), Version: version, Name: pkg.Name, Description: pkg.Description, Directory: pkg.Directory, SizeBytes: pkg.Size, SHA256: pkg.SHA256, CreatedAt: time.Now().UTC()}}, nil
}

func validSystemVersion(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if len(value) > 64 || value == "latest" {
		return false
	}
	for i, r := range value {
		if i == 0 && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (h *Handler) withSystemSkillWrite(ctx context.Context, actor string, write func(*db.PlatformAdminTx) error) error {
	return h.db.WithPlatformAdminTx(ctx, func(tx *db.PlatformAdminTx) error {
		granted, err := tx.LockGrant(ctx, actor)
		if errors.Is(err, db.ErrNotFound) || (err == nil && !granted) {
			return errSystemSkillDenied
		}
		if err != nil {
			return err
		}
		return write(tx)
	})
}

func (h *Handler) changeSystemIcon(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		IconKey  string `json:"icon_key"`
		Revision int64  `json:"revision"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1024), &input); err != nil {
		return systemInvalid("图标参数无效")
	}
	key, err := skillIconKey(input.IconKey)
	if err != nil {
		return err
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	var skill db.SystemSkill
	err = h.withSystemSkillWrite(r.Context(), principal.UserUUID, func(tx *db.PlatformAdminTx) error {
		var err error
		skill, err = tx.ChangeSystemSkill(r.Context(), db.SystemSkillChange{ActorUUID: principal.UserUUID, SkillID: chi.URLParam(r, "skill_id"), Revision: input.Revision, Action: "set_icon", IconKey: key})
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, 200, systemSkillDTO(skill))
	return nil
}
