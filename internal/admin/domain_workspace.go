package admin

import (
	"errors"
	"strings"
)

func validateWorkspaceRole(role string, allowBilling bool) error {
	switch role {
	case "workspace_user", "workspace_developer", "workspace_restricted_developer", "workspace_admin":
		return nil
	case "workspace_billing":
		if allowBilling {
			return nil
		}
	}
	return errors.New("invalid workspace role")
}

func validateTags(tags map[string]string) error {
	for key := range tags {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			return errors.New("tag keys must be non-empty")
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "anthropic") {
			return errors.New("tag keys may not begin with anthropic")
		}
	}
	return nil
}
