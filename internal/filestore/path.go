package filestore

import (
	"errors"
	"regexp"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
)

var (
	filesystemIDPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[a-zA-Z0-9]+(?:_[a-zA-Z0-9]+)+)$`)
	mediaTypePattern    = regexp.MustCompile(`^[a-zA-Z0-9.+-]+/[a-zA-Z0-9.+-]+$`)
	intentPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
)

func validateFilesystemID(value string) error {
	if !filesystemIDPattern.MatchString(value) {
		return errors.New("filesystemId is invalid")
	}
	return nil
}

func validateFilestorePath(value string, allowRoot bool) error {
	return filestorepath.Validate(value, allowRoot)
}

func parentPath(value string) string {
	return filestorepath.Parent(value)
}

func isDescendant(candidate, ancestor string) bool {
	return filestorepath.IsDescendant(candidate, ancestor)
}

func validateMediaType(value string) error {
	if !mediaTypePattern.MatchString(value) {
		return errors.New("mediaType is invalid")
	}
	return nil
}

func validateAuthorizationMetadata(value *authorizationMetadata) error {
	if value == nil || value.Intent == "" {
		return nil
	}
	if !intentPattern.MatchString(value.Intent) {
		return errors.New("authorizationMetadata.intent is invalid")
	}
	return nil
}
