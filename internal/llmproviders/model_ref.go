package llmproviders

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultSlug      = "default"
	OrganizationSlug = "organization"
)

var (
	ErrInvalidSlug = errors.New("slug must contain 1 to 32 lowercase letters, digits, '.', '_' or '-' and start with a letter or digit")
	slugPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
)

func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return ErrInvalidSlug
	}
	return nil
}

func QualifiedModelID(slug, upstreamModelID string) string {
	return slug + "/" + upstreamModelID
}

func ParseModelRef(modelID string) (string, string, bool) {
	slug, upstreamModelID, found := strings.Cut(modelID, "/")
	if !found || slug == "" || upstreamModelID == "" {
		return "", "", false
	}
	return slug, upstreamModelID, true
}

func NextDefaultSlug(taken []string) string {
	used := make(map[string]struct{}, len(taken))
	for _, slug := range taken {
		used[slug] = struct{}{}
	}
	candidate := DefaultSlug
	for position := 2; ; position++ {
		if _, exists := used[candidate]; !exists {
			return candidate
		}
		candidate = DefaultSlug + "-" + strconv.Itoa(position)
	}
}
