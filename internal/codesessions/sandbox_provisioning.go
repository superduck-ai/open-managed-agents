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

func (h *Handler) ensureSessionSandbox(ctx context.Context, session db.CodeSession, renew bool) (string, error) {
	renewed := !renew
	if renew {
		sandbox, err := h.db.GetResumableEnvironmentSandboxForCodeSession(ctx, session.ExternalID)
		if err == nil {
			if err := h.renewStaleSandboxTimeout(ctx, sandbox); err != nil {
				return "", err
			}
			renewed = true
		} else if !errors.Is(err, db.ErrNotFound) {
			return "", err
		}
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
	if err := h.renewStaleSandboxTimeout(ctx, sandbox); err != nil {
		return "", err
	}
	return endpoint, nil
}
