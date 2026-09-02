package platformapi

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func invalidEmailLoginRequest(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Invalid email login request", cause)
}
