package codesessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type SandboxProvisioner interface {
	EnsureSessionSandbox(context.Context, db.CodeSession) (string, error)
}

func (s *Service) WithSandboxProvisioner(provisioner SandboxProvisioner) *Service {
	s.sandboxProvisioner = provisioner
	return s
}

func (h *Handler) ensureSessionSandbox(ctx context.Context, session db.CodeSession) (string, error) {
	renewed := false
	if sandbox, err := h.db.GetResumableEnvironmentSandboxForCodeSession(ctx, session.ExternalID); err == nil {
		if err := h.renewSandboxTimeout(ctx, sandbox); err != nil {
			return "", err
		}
		renewed = true
	} else if !errors.Is(err, db.ErrNotFound) {
		return "", err
	}
	if h.service.sandboxProvisioner == nil {
		return "", errSandboxUnavailable
	}
	endpoint, err := h.service.sandboxProvisioner.EnsureSessionSandbox(ctx, session)
	if err != nil {
		return "", errors.Join(errSandboxUnavailable, err)
	}
	if renewed {
		return endpoint, nil
	}
	sandbox, err := h.db.GetResumableEnvironmentSandboxForCodeSession(ctx, session.ExternalID)
	if err != nil {
		return "", err
	}
	if err := h.renewSandboxTimeout(ctx, sandbox); err != nil {
		return "", err
	}
	return endpoint, nil
}
