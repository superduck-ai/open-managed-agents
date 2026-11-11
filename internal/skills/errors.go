package skills

import (
	"errors"
	"fmt"
	"net/http"

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
func skillLastVersionRequired() error {
	return apperr.New(apperr.InvalidArgument, "Cannot delete the only skill version; upload a replacement or delete the skill", nil)
}
func invalidSkillLabel(field string, limit int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("%s must be a single line with at most %d characters", field, limit), nil)
}

func skillNameImmutable() error {
	return apperr.New(apperr.InvalidArgument, "Skill name cannot change across versions", nil)
}

var errSystemSkillDenied = errors.New("system skill administration denied")

type systemSkillPublicError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *systemSkillPublicError) Error() string { return e.Message }

func systemInvalid(message string) error {
	return &systemSkillPublicError{Status: 400, Code: "invalid_request", Message: message}
}

func systemSkillError(err error) *systemSkillPublicError {
	if app, ok := errors.AsType[*apperr.Error](err); ok {
		statuses := map[apperr.Kind]int{apperr.InvalidArgument: 400, apperr.Unauthenticated: 401, apperr.PermissionDenied: 403, apperr.NotFound: 404, apperr.Conflict: 409, apperr.RequestTooLarge: 413}
		if status, found := statuses[app.Kind]; found {
			return &systemSkillPublicError{Status: status, Code: "skill_request_failed", Message: app.PublicMessage}
		}
	}
	if public, ok := errors.AsType[*systemSkillPublicError](err); ok {
		return public
	}
	if pkg, ok := errors.AsType[packageError](err); ok {
		return &systemSkillPublicError{Status: pkg.Status, Code: "invalid_package", Message: pkg.Message}
	}
	switch {
	case errors.Is(err, db.ErrBundledSkillReadOnly):
		return &systemSkillPublicError{Status: 403, Code: "bundled_skill_read_only", Message: "项目预置技能只允许上架或下架"}
	case errors.Is(err, errSystemSkillDenied):
		return &systemSkillPublicError{Status: 403, Code: "platform_access_denied", Message: "仅超级管理员可管理系统技能"}
	case errors.Is(err, db.ErrNotFound):
		return &systemSkillPublicError{Status: 404, Code: "not_found", Message: "技能或版本不存在"}
	case errors.Is(err, db.ErrSystemSkillRevision):
		return &systemSkillPublicError{Status: 409, Code: "revision_conflict", Message: "技能已被更新，请刷新后重新确认"}
	case errors.Is(err, db.ErrVersionConflict):
		return &systemSkillPublicError{Status: 409, Code: "version_conflict", Message: "相同版本已存在且内容不同，请使用新版本号"}
	case errors.Is(err, db.ErrSystemSkillNameMismatch):
		return &systemSkillPublicError{Status: 400, Code: "skill_name_mismatch", Message: "技能名称必须与目标技能名称一致"}
	case errors.Is(err, db.ErrInvalidState):
		return &systemSkillPublicError{Status: 409, Code: "state_conflict", Message: "当前状态不允许此操作，请检查目标技能和已发布版本"}
	default:
		return &systemSkillPublicError{Status: 503, Code: "unavailable", Message: "系统技能服务暂不可用，请稍后重试"}
	}
}

func systemSkillPreviewTooLarge() error {
	return &systemSkillPublicError{Status: 413, Code: "preview_too_large", Message: "SKILL.md 超过 256 KiB 预览限制"}
}

func invalidSkillIcon() error {
	return apperr.New(apperr.InvalidArgument, "请选择有效的技能图标", nil)
}

func consoleSkillDenied() error {
	return apperr.New(apperr.PermissionDenied, "Workspace access required", nil)
}

func skillPreviewFileNotFound() error {
	return apperr.New(apperr.NotFound, "文件不存在", nil)
}
