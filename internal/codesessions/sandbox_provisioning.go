package codesessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type SandboxProvisioner interface {
	EnsureSessionSandbox(context.Context, db.CodeSession) (string, db.EnvironmentSandbox, error)
}

func (s *Service) WithSandboxProvisioner(provisioner SandboxProvisioner) *Service {
	s.sandboxProvisioner = provisioner
	return s
}

func (h *Handler) ensureSessionSandbox(ctx context.Context, session db.CodeSession, renew bool) (string, error) {
	if h.service.sandboxProvisioner == nil {
		return "", errSandboxUnavailable
	}
	endpoint, sandbox, err := h.service.sandboxProvisioner.EnsureSessionSandbox(ctx, session)
	if err != nil {
		return "", errors.Join(errSandboxUnavailable, err)
	}
	if renew {
		if err := h.renewStaleSandboxTimeout(ctx, sandbox); err != nil {
			return "", err
		}
	}
	return endpoint, nil
}
