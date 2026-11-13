package skills

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var ErrInvalidSkillReference = errors.New("invalid Skill reference")

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func skillRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func skillAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func requireWorkspaceCredential(principal auth.Principal) error {
	if principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession {
		return nil
	}
	return apperr.New(apperr.PermissionDenied, "Credential cannot access skills", nil)
}

func readOnlyBuiltinError() error {
	return apperr.New(apperr.InvalidArgument, "Built-in skills are read-only", nil)
}

func skillNotFound(skillID string, cause error) error {
	return apperr.New(apperr.NotFound, "Skill not found: "+skillID, cause)
}

func skillVersionNotFound(version string, cause error) error {
	return apperr.New(apperr.NotFound, "Skill version not found: "+version, cause)
}

func skillDisplayTitleConflict(displayTitle string, cause error) error {
	return apperr.New(apperr.InvalidArgument, "Skill cannot reuse an existing display_title: "+displayTitle, cause)
}

func mapSkillPackageError(err error) error {
	var packageErr packageError
	if !errors.As(err, &packageErr) {
		return internalError("Could not read skill package", fmt.Errorf("read skill package: %w", err))
	}
	if packageErr.Status == http.StatusRequestEntityTooLarge {
		return apperr.New(apperr.RequestTooLarge, packageErr.Message, err)
	}
	return apperr.New(apperr.InvalidArgument, packageErr.Message, err)
}

func skillDownloadError(cause error) error {
	return internalError("Could not download skill version", cause)
}

func mapResolveVersionError(skillID, version string, err error) error {
	if errors.Is(err, db.ErrNotFound) {
		if version == "latest" {
			return skillNotFound(skillID, err)
		}
		return skillVersionNotFound(version, err)
	}
	return internalError("Could not retrieve skill version", fmt.Errorf("resolve skill %q version %q: %w", skillID, version, err))
}

func skillHasNoVersions() error {
	return apperr.New(apperr.InvalidArgument, "Skill has no current version", nil)
}
func skillVersionsMustBeDeleted() error {
	return apperr.New(apperr.InvalidArgument, "Delete all skill versions before deleting the skill", nil)
}
func skillInUseByAgents(inUse *db.SkillInUseError) error {
	labels := make([]string, 0, len(inUse.Agents))
	for _, agent := range inUse.Agents {
		name := agent.Name
		if name != "" {
			labels = append(labels, fmt.Sprintf("%s (%s)", name, agent.ExternalID))
		} else {
			labels = append(labels, agent.ExternalID)
		}
	}
	return apperr.New(
		apperr.Conflict,
		fmt.Sprintf("Skill is in use by active agent(s): %s. Remove it from those agents before deleting. If used by a subagent, remove that subagent reference or select a version without this skill.", strings.Join(labels, ", ")),
		inUse,
	)
}
func skillLastVersionRequired() error {
	return apperr.New(apperr.InvalidArgument, "Cannot delete the only skill version; upload a replacement or delete the skill", nil)
}
func invalidSkillLabel(field string, limit int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("%s must be a single line with at most %d characters", field, limit), nil)
}

func skillNameImmutable() error {
	return apperr.New(apperr.InvalidArgument, "Skill name cannot change across versions", nil)
}

var (
	errSystemSkillDenied     = errors.New("system skill administration denied")
	errInvalidSkillIcon      = errors.New("invalid skill icon")
	errSkillPreviewNotFound  = errors.New("skill preview file not found")
	errSkillPreviewTooLarge  = errors.New("skill preview exceeds limit")
	errSkillStorageMismatch  = errors.New("skill storage configuration mismatch")
	errSkillIntegrityFailed  = errors.New("skill package integrity check failed")
	errConsoleSkillDenied    = errors.New("workspace access required")
	errInvalidSkillFilter    = errors.New("invalid skill filter")
	errInvalidSkillLimit     = errors.New("invalid skill paging limit")
	errInvalidSkillOffset    = errors.New("invalid skill paging offset")
	errInvalidSkillIconInput = errors.New("invalid skill icon payload")
)

type systemSkillPublicError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *systemSkillPublicError) Error() string { return e.Message }

type skillErrorRule struct {
	target  error
	status  int
	code    string
	message string
}

var systemSkillErrorRules = []skillErrorRule{
	{db.ErrBundledSkillReadOnly, 403, "bundled_skill_read_only", "项目预置技能不能删除，可上传更新或下架"},
	{errSystemSkillDenied, 403, "platform_access_denied", "仅超级管理员可管理系统技能"},
	{db.ErrNotFound, 404, "not_found", "技能或版本不存在"},
	{db.ErrVersionConflict, 409, "version_conflict", "相同版本已存在且内容不同，请使用新版本号"},
	{db.ErrSystemSkillNameMismatch, 400, "skill_name_mismatch", "技能名称必须与目标技能名称一致"},
	{db.ErrInvalidState, 409, "state_conflict", "当前状态不允许此操作，请检查目标技能"},
	{errInvalidSkillIcon, 400, "skill_request_failed", "请选择有效的技能图标"},
	{errSkillPreviewNotFound, 404, "skill_request_failed", "文件不存在"},
	{errSkillPreviewTooLarge, 413, "preview_too_large", "SKILL.md 超过 256 KiB 预览限制"},
	{errSkillStorageMismatch, 400, "invalid_request", "技能存储配置不匹配"},
	{errSkillIntegrityFailed, 400, "invalid_request", "技能包完整性检查失败"},
	{errInvalidSkillLimit, 400, "invalid_request", "分页数量必须为 1–100"},
	{errInvalidSkillOffset, 400, "invalid_request", "分页位置无效"},
	{errInvalidSkillIconInput, 400, "invalid_request", "图标参数无效"},
}

func systemInvalid(message string) error {
	return &systemSkillPublicError{Status: 400, Code: "invalid_request", Message: message}
}

func systemSkillError(err error) *systemSkillPublicError {
	if public, ok := errors.AsType[*systemSkillPublicError](err); ok {
		return public
	}
	for _, rule := range systemSkillErrorRules {
		if errors.Is(err, rule.target) {
			return &systemSkillPublicError{Status: rule.status, Code: rule.code, Message: rule.message}
		}
	}
	if app, ok := errors.AsType[*apperr.Error](err); ok {
		statuses := map[apperr.Kind]int{apperr.InvalidArgument: 400, apperr.Unauthenticated: 401, apperr.PermissionDenied: 403, apperr.NotFound: 404, apperr.Conflict: 409, apperr.RequestTooLarge: 413}
		if status, found := statuses[app.Kind]; found {
			return &systemSkillPublicError{Status: status, Code: "skill_request_failed", Message: app.PublicMessage}
		}
	}
	if pkg, ok := errors.AsType[packageError](err); ok {
		return &systemSkillPublicError{Status: pkg.Status, Code: "invalid_package", Message: pkg.Message}
	}
	return &systemSkillPublicError{Status: 503, Code: "unavailable", Message: "系统技能服务暂不可用，请稍后重试"}
}

func mapConsoleSkillError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*apperr.Error](err); ok {
		return err
	}
	if pkg, ok := errors.AsType[packageError](err); ok {
		return mapSkillPackageError(pkg)
	}
	switch {
	case errors.Is(err, errConsoleSkillDenied):
		return apperr.New(apperr.PermissionDenied, "Workspace access required", err)
	case errors.Is(err, db.ErrNotFound):
		return apperr.New(apperr.NotFound, "Skill or version not found", err)
	case errors.Is(err, errSkillPreviewNotFound):
		return apperr.New(apperr.NotFound, "File not found", err)
	case errors.Is(err, errSkillPreviewTooLarge):
		return apperr.New(apperr.RequestTooLarge, "SKILL.md exceeds 256 KiB preview limit", err)
	case errors.Is(err, errInvalidSkillIconInput):
		return apperr.New(apperr.InvalidArgument, "Invalid icon request payload", err)
	case errors.Is(err, errSkillStorageMismatch):
		return apperr.New(apperr.InvalidArgument, "Skill storage configuration mismatch", err)
	case errors.Is(err, errSkillIntegrityFailed):
		return apperr.New(apperr.InvalidArgument, "Skill package integrity check failed", err)
	case errors.Is(err, errInvalidSkillFilter):
		return apperr.New(apperr.InvalidArgument, "Invalid skill filter parameters", err)
	case errors.Is(err, errInvalidSkillLimit):
		return apperr.New(apperr.InvalidArgument, "Limit must be between 1 and 100", err)
	case errors.Is(err, errInvalidSkillOffset):
		return apperr.New(apperr.InvalidArgument, "Offset is invalid", err)
	default:
		return err
	}
}

func systemSkillPreviewTooLarge() error {
	return errSkillPreviewTooLarge
}

func invalidSkillIcon() error {
	return apperr.New(apperr.InvalidArgument, "Please choose a valid skill icon", errInvalidSkillIcon)
}

func skillPreviewFileNotFound() error {
	return errSkillPreviewNotFound
}
