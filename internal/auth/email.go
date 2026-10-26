package auth

import (
	"slices"
	"strings"
)

func EmailDomainAllowed(email string, allowedDomains []string) bool {
	if len(allowedDomains) == 0 {
		return true
	}
	_, domain, ok := strings.CutLast(email, "@")
	return ok && slices.Contains(allowedDomains, strings.ToLower(domain))
}
