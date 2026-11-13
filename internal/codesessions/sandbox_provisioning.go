package codesessions

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"golang.org/x/sync/singleflight"
)

type SandboxProvisioner interface {
	EnsureSessionSandbox(context.Context, db.CodeSession) (db.EnvironmentSandbox, error)
}

func (s *Service) WithSandboxProvisioner(provisioner SandboxProvisioner) *Service {
	s.sandboxProvisioner = provisioner
	return s
}

func (h *Handler) ensureSessionSandbox(ctx context.Context, session db.CodeSession, renew bool) error {
	if h.service.sandboxProvisioner == nil {
		return errSandboxUnavailable
	}
	provisioning := h.sandboxEnsures.DoChan(session.UUID, func() (any, error) {
		return h.service.sandboxProvisioner.EnsureSessionSandbox(context.WithoutCancel(ctx), session)
	})
	var outcome singleflight.Result
	select {
	case <-ctx.Done():
		return errors.Join(errSandboxUnavailable, ctx.Err())
	case outcome = <-provisioning:
	}
	if outcome.Err != nil {
		return errors.Join(errSandboxUnavailable, outcome.Err)
	}
	sandbox, _ := outcome.Val.(db.EnvironmentSandbox)
	if renew {
		if err := h.renewStaleSandboxTimeout(ctx, sandbox); err != nil {
			return err
		}
	}
	return nil
}
