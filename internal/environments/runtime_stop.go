package environments

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

type sandboxKiller interface {
	Kill(ctx context.Context, sandboxID string) error
}

type sessionRuntimeStore interface {
	ListUnstoppedEnvironmentWorkBySession(ctx context.Context, workspaceUUID, sessionUUID string) ([]db.EnvironmentWork, error)
	GetActiveEnvironmentSandboxForWork(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string) (db.EnvironmentSandbox, error)
	UpdateEnvironmentSandboxState(ctx context.Context, workspaceUUID, externalID, state string, providerSandboxID *string, lastError *string, stoppedAt *time.Time) error
	StopEnvironmentWork(ctx context.Context, workspaceUUID, environmentExternalID, workExternalID string, force bool) (db.EnvironmentWork, error)
}

// SessionRuntimeStopper kills leftover sandboxes for a Session and force-stops
// its Environment Work. Dream workers use it after the public contract is
// already terminal; HTTP force-stop reuses the same kill helper.
type SessionRuntimeStopper struct {
	store  sessionRuntimeStore
	killer sandboxKiller
}

func (s *SessionRuntimeStopper) StopSessionRuntime(ctx context.Context, workspaceUUID, sessionUUID string) error {
	if s == nil || s.store == nil {
		return nil
	}
	works, err := s.store.ListUnstoppedEnvironmentWorkBySession(ctx, workspaceUUID, sessionUUID)
	if err != nil {
		return err
	}
	var errs []error
	for _, work := range works {
		if err := s.stopWork(ctx, work); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *SessionRuntimeStopper) stopWork(ctx context.Context, work db.EnvironmentWork) error {
	if err := killActiveSandboxForWork(ctx, s.store, s.killer, work); err != nil {
		return err
	}
	_, err := s.store.StopEnvironmentWork(ctx, work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID, true)
	return err
}

func killActiveSandboxForWork(ctx context.Context, store sessionRuntimeStore, killer sandboxKiller, work db.EnvironmentWork) error {
	if store == nil {
		return nil
	}
	sandbox, err := store.GetActiveEnvironmentSandboxForWork(ctx, work.WorkspaceUUID, work.EnvironmentExternalID, work.ExternalID)
	if errors.Is(err, db.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if sandbox.ProviderSandboxID == nil || *sandbox.ProviderSandboxID == "" {
		return nil
	}
	providerSandboxID := *sandbox.ProviderSandboxID
	if err := store.UpdateEnvironmentSandboxState(ctx, work.WorkspaceUUID, sandbox.ExternalID, "stopping", &providerSandboxID, nil, nil); err != nil {
		return err
	}
	if killer != nil {
		if err := killer.Kill(ctx, providerSandboxID); err != nil && !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
			message := err.Error()
			_ = store.UpdateEnvironmentSandboxState(ctx, work.WorkspaceUUID, sandbox.ExternalID, "stopping", &providerSandboxID, &message, nil)
			return err
		}
	}
	stoppedAt := time.Now().UTC()
	return store.UpdateEnvironmentSandboxState(ctx, work.WorkspaceUUID, sandbox.ExternalID, "stopped", &providerSandboxID, nil, &stoppedAt)
}
