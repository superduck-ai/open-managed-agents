package skills

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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
